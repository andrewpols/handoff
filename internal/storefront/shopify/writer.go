package shopify

import (
	"errors"
	"net/http"

	"github.com/andrewpols/handoff/internal/model"
)

type ShopifyWriter struct {
	client http.Client
}

func (sw *ShopifyWriter) WriteProducts([]model.Product) error {
	return errors.New("")
}

func (sw *ShopifyWriter) WriteCustomers([]model.Customer) error {
	return errors.New("")
}

func (sw *ShopifyWriter) WriteOrders([]model.Order) error {
	return errors.New("")
}
