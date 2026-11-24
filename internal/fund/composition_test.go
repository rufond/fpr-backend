package fund

import "testing"

func TestAggregateCompositionRowsPreservesDecimalScaleAndBounds(t *testing.T) {
	items := []compositionRow{
		{ISIN: "KZ1C00001122", Quantity: new("1.0000"), AssetSharePercent: "0.1000"},
		{ISIN: "KZ1C00001122", Quantity: new("0.002"), AssetSharePercent: "0.230000", AssetShareUpperBound: true},
		{ISIN: "KZ1C00001122", AssetSharePercent: "0.000010"},
	}
	holding, err := aggregateCompositionRows(items)
	if err != nil {
		t.Fatal(err)
	}
	if holding.Quantity != "1.0020" || holding.AssetSharePercent != "0.330010" || !holding.AssetShareUpperBound || holding.Rows != 3 {
		t.Fatalf("unexpected aggregate: %#v", holding)
	}
}
