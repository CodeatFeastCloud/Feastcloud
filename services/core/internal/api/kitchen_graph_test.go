// SPDX-License-Identifier: AGPL-3.0-only

package api

import "testing"

func TestMenuItemInputAllowsAnUnlinkedRecipe(t *testing.T) {
	input := menuItemInput{
		ID: "11111111-1111-4111-8111-111111111111", OutletID: "22222222-2222-4222-8222-222222222222",
		Name: "Imported masala dosa", Code: "DOSA-1", PriceMinor: 12050, Currency: "INR",
	}
	if err := input.validate(); err != nil {
		t.Fatalf("recipe-less imported menu item should be valid: %v", err)
	}
}
