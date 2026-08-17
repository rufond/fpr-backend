package prices

import "errors"

var (
	ErrPriceSourceInstrumentNotFound = errors.New("price source instrument is not in current fund composition")
	ErrUnsupportedPriceProvider      = errors.New("unsupported price provider")
	ErrPriceProviderNotImplemented   = errors.New("price provider is not implemented")
)
