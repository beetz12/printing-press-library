// Package ap2 implements AP2 (Agent Payments Protocol) mandate types and builders.
// Lifted from ucp-pp-cli/internal/ucp/types.go (Cart, LineItem, Item, Buyer, Total).
package ap2

// Cart is the local representation of a cart used by BuildCartMandate.
type Cart struct {
	ID        string     `json:"id"`
	Merchant  string     `json:"merchant"`
	Currency  string     `json:"currency,omitempty"`
	LineItems []LineItem `json:"line_items"`
	Buyer     *Buyer     `json:"buyer,omitempty"`
	Totals    []Total    `json:"totals,omitempty"`
	Status    string     `json:"status,omitempty"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

// LineItem is one item in a cart or checkout session.
type LineItem struct {
	ID       string  `json:"id"`
	Item     Item    `json:"item"`
	Quantity int     `json:"quantity"`
	Totals   []Total `json:"totals,omitempty"`
}

// Item describes a product.
type Item struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Price     int    `json:"price"` // minor units (cents)
	SKU       string `json:"sku,omitempty"`
	GTIN      string `json:"gtin,omitempty"`
	URL       string `json:"url,omitempty"`
	VariantID int64  `json:"variant_id,omitempty"`
}

// Buyer is the shopper identity attached to a cart/checkout.
type Buyer struct {
	FullName string `json:"full_name,omitempty"`
	Email    string `json:"email,omitempty"`
}

// Total is one entry in a totals array (subtotal, discount, tax, shipping, total).
type Total struct {
	Type   string `json:"type"`
	Amount int    `json:"amount"` // minor units
}
