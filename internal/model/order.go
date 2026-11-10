package model

type Order struct {
	LineItems []LineItem
	Customer  Customer
	Currency  string
}
