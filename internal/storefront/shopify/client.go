package shopify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

var API_VERSION = "2026-10"

var (
	BULK_UPLOAD_TIMEOUT = 5 * time.Minute
	BULK_POLL_INTERVAL  = 2 * time.Second
	BULK_POLL_TIMEOUT   = 30 * time.Minute
)

type authTransport struct {
	token string
	base  http.RoundTripper
}

type ShopifyClient struct {
	URL string
	http.Client
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func newShopifyClient(storeName string, token string) *ShopifyClient {
	url := "https://" + storeName + ".myshopify.com/admin/" + API_VERSION + "/graphql.json"

	transport := authTransport{
		token: token,
		base:  http.DefaultTransport,
	}

	return &ShopifyClient{
		URL:       url,
		Timeout:   10 * time.Second,
		Transport: &transport,
	}
}

// NoMorePages is the cursor Search returns once the last page has been read.
// An empty cursor requests the first page.
const NoMorePages = "-1"

// Search fetches one page of the root connection (e.g. "products") and returns
// the deserialized nodes along with the cursor for the next page.
func Search[T any, U any](client *ShopifyClient, query string, root string, deserialize func([]T) []U, after string) ([]U, string, error) {
	variables := map[string]interface{}{}
	if after != "" {
		variables["after"] = after
	}

	data, err := execute(client, ShopifyWritePayload{Query: query, Variables: variables})
	if err != nil {
		return nil, after, err
	}

	var connections map[string]Connection[T]
	if err := json.Unmarshal(data, &connections); err != nil {
		return nil, after, fmt.Errorf("parsing %s response: %w", root, err)
	}
	conn, ok := connections[root]
	if !ok {
		return nil, after, fmt.Errorf("shopify response missing %q", root)
	}

	next := NoMorePages
	if conn.PageInfo.HasNextPage {
		next = conn.PageInfo.EndCursor
	}

	return deserialize(conn.Nodes), next, nil
}

// Write serializes items into GraphQL payloads and posts each one in turn.
func Write[U any](client *ShopifyClient, serialize func([]U) []ShopifyWritePayload, items []U) error {
	for _, payload := range serialize(items) {
		if _, err := execute(client, payload); err != nil {
			return err
		}
	}

	return nil
}

// execute posts a GraphQL payload and returns the response's data field.
// Top-level errors and mutation userErrors are both surfaced as Go errors.
func execute(client *ShopifyClient, payload ShopifyWritePayload) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, client.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("forming request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("shopify returned %d: %s", resp.StatusCode, string(respBody))
	}

	// GraphQL reports failures with a 200 and an "errors" array
	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("shopify graphql error: %s", result.Errors[0].Message)
	}

	var mutations map[string]struct {
		UserErrors []struct {
			Message string `json:"message"`
		} `json:"userErrors"`
	}
	if err := json.Unmarshal(result.Data, &mutations); err == nil {
		for name, result := range mutations {
			if len(result.UserErrors) > 0 {
				return nil, fmt.Errorf("%s failed: %s", name, result.UserErrors[0].Message)
			}
		}
	}

	return result.Data, nil
}

// BulkWrite runs every payload through one Shopify bulk mutation: the payloads'
// variables are uploaded as JSONL and Shopify applies the shared mutation to each
// line. It blocks until the operation finishes. All payloads must share the same
// Query: only the first payload's Query is used.
//
// Phases: Reserve the space for the upload, stage the variables file, start the operation, then poll.
// COMPLETED means the job finished, not that every line succeeded;
// per-line failures are in the operation's result file, which is not fetched here.
func BulkWrite[U any](client *ShopifyClient, serialize func([]U) []ShopifyWritePayload, items []U) error {
	payloads := serialize(items)
	if len(payloads) == 0 {
		return nil
	}

	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	for _, payload := range payloads {
		if err := enc.Encode(payload.Variables); err != nil {
			return fmt.Errorf("encoding bulk variables: %w", err)
		}
	}

	stagedPath, err := stageUpload(client, &jsonl)
	if err != nil {
		return err
	}

	data, err := execute(client, ShopifyWritePayload{
		Query: BulkOperationRunMutation,
		Variables: map[string]interface{}{
			"mutation":         payloads[0].Query,
			"stagedUploadPath": stagedPath,
		},
	})
	if err != nil {
		return err
	}

	var operation struct {
		BulkOperationRunMutation struct {
			BulkOperation struct {
				ID string `json:"id"`
			} `json:"bulkOperation"`
		} `json:"bulkOperationRunMutation"`
	}
	if err := json.Unmarshal(data, &operation); err != nil {
		return err
	}

	return waitForBulkOperation(client, operation.BulkOperationRunMutation.BulkOperation.ID)
}

// stageUpload uploads the JSONL body to a Shopify staged target and returns
// the path to hand to bulkOperationRunMutation. It first asks Shopify for an
// upload target (GraphQL), then POSTs the file straight to that storage URL,
// without our auth headers since the signed parameters authorize it.
func stageUpload(client *ShopifyClient, body *bytes.Buffer) (string, error) {
	data, err := execute(client, ShopifyWritePayload{
		Query: StagedUploadsCreateMutation,
		Variables: map[string]interface{}{
			"input": []map[string]interface{}{{
				"resource":   "BULK_MUTATION_VARIABLES",
				"filename":   "bulk_op_vars",
				"mimeType":   "text/jsonl",
				"httpMethod": "POST",
			}},
		},
	})
	if err != nil {
		return "", err
	}

	var staged struct {
		StagedUploadsCreate struct {
			StagedTargets []struct {
				URL        string `json:"url"`
				Parameters []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"parameters"`
			} `json:"stagedTargets"`
		} `json:"stagedUploadsCreate"`
	}
	if err := json.Unmarshal(data, &staged); err != nil {
		return "", err
	}
	if len(staged.StagedUploadsCreate.StagedTargets) == 0 {
		return "", errors.New("shopify returned no staged upload target")
	}
	target := staged.StagedUploadsCreate.StagedTargets[0]

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	key := ""
	for _, param := range target.Parameters {
		if param.Name == "key" {
			key = param.Value
		}
		if err := writer.WriteField(param.Name, param.Value); err != nil {
			return "", err
		}
	}
	// the file part must come after the parameters
	file, err := writer.CreateFormFile("file", "bulk_op_vars")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, body); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, target.URL, &form)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// the staged target is not Shopify's API, so it gets a plain client without our auth headers
	uploader := http.Client{Timeout: BULK_UPLOAD_TIMEOUT}
	resp, err := uploader.Do(req)
	if err != nil {
		return "", fmt.Errorf("uploading bulk variables: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bulk upload returned %d: %s", resp.StatusCode, string(respBody))
	}

	return key, nil
}

// waitForBulkOperation polls until the bulk operation with the given ID finishes.
// It looks the operation up by ID and only reads status; the writes happen on
// Shopify's side.
func waitForBulkOperation(client *ShopifyClient, id string) error {
	deadline := time.Now().Add(BULK_POLL_TIMEOUT)

	for time.Now().Before(deadline) {
		data, err := execute(client, ShopifyWritePayload{
			Query:     BulkOperationQuery,
			Variables: map[string]interface{}{"id": id},
		})
		if err != nil {
			return err
		}

		var result struct {
			Node *struct {
				Status    string `json:"status"`
				ErrorCode string `json:"errorCode"`
			} `json:"node"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return err
		}
		if result.Node == nil {
			return fmt.Errorf("bulk operation %s not found", id)
		}

		switch result.Node.Status {
		case "COMPLETED":
			return nil
		case "CREATED", "RUNNING":
			// do nothing
		default:
			return fmt.Errorf("bulk operation %s ended with status %s (%s)", id, result.Node.Status, result.Node.ErrorCode)
		}

		time.Sleep(BULK_POLL_INTERVAL)
	}

	return fmt.Errorf("bulk operation %s did not finish within %s", id, BULK_POLL_TIMEOUT)
}
