package storefront

import (
	"github.com/andrewpols/handoff/internal/model"
)

type Reader interface {
	Products(page string) ([]model.Product, string, error)
	Customers(page string) ([]model.Customer, string, error)
	Orders(page string) ([]model.Order, string, error)
}

type Writer interface {
	WriteProducts([]model.Product) error
	WriteCustomers([]model.Customer) error
	WriteOrders([]model.Order) error
}

type Storefront interface {
	Reader
	Writer
}
