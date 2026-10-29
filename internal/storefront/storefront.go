package storefront

import (
	"github.com/andrewpols/handoff/internal/models"
)

type Reader interface {
	Products(page string) []models.Product
	Customers(page string)
	Orders(page string)
}

type Writer interface {
	Write()
}

type Storefront interface {
	Reader
	Writer
}
