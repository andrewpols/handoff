package shopify

type ShopifyStorefront struct {
	reader ShopifyReader
	writer ShopifyWriter
}

func newShopifyStorefront() *ShopifyStorefront {
	client := newShopifyClient()
	return &ShopifyStorefront{
		reader: ShopifyReader{client: *client},
		writer: ShopifyWriter{client: *client},
	}
}
