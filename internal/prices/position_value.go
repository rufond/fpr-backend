package prices

import (
	"math/big"

	"github.com/rufond/fpr-backend/internal/appstate"
	"github.com/rufond/fpr-backend/internal/decimal"
)

// PositionValues returns quote currency values for holdings with a normalized
// market price. The quote and the holding quantity must describe the same unit.
func PositionValues(fund *appstate.FundState, quotes []appstate.InstrumentPrice) map[int64]string {
	values := make(map[int64]string, len(quotes))
	if fund == nil || len(quotes) == 0 {
		return values
	}

	quantities := PositionQuantities(fund)
	for _, quote := range quotes {
		quantity := quantities[quote.InstrumentID]
		unitValue, ok := decimal.Parse(quote.UnitValue)
		if quantity != nil && ok {
			values[quote.InstrumentID] = new(big.Rat).Mul(quantity, unitValue).FloatString(8)
		}
	}

	return values
}

func PositionQuantities(fund *appstate.FundState) map[int64]*big.Rat {
	quantities := make(map[int64]*big.Rat, len(fund.Snapshot.Assets))
	for _, asset := range fund.Snapshot.Assets {
		if asset.InstrumentID == nil {
			continue
		}

		quantity, ok := decimal.Parse(asset.Quantity)
		if !ok {
			continue
		}

		id := *asset.InstrumentID
		if previous := quantities[id]; previous != nil {
			quantity.Add(quantity, previous)
		}

		quantities[id] = quantity
	}

	return quantities
}
