package model

type Product struct {
	Title       string
	Description string
	Variants    []ProductVariant
}

type ProductVariant struct {
	SKU               string
	InventoryQuantity uint
	Price             string
	LocationID        string
}
