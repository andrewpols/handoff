package shopify

import (
	"errors"
	"net/http"

	"github.com/andrewpols/handoff/internal/model"
)

type ShopifyReader struct {
	client http.Client
}

func (sw *ShopifyReader) Products(page string) ([]model.Product, string, error) {
	return []model.Product{}, "", errors.New("")
}

func (sw *ShopifyReader) Customers(page string) ([]model.Customer, string, error) {
	return []model.Customer{}, "", errors.New("")
}

func (sw *ShopifyReader) Orders(page string) ([]model.Order, string, error) {
	return []model.Order{}, "", errors.New("")
}
