package nbk

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rufond/fpr-backend/internal/decimal"
	"github.com/rufond/fpr-backend/internal/fx"
)

var almaty = time.FixedZone("Asia/Almaty", 5*60*60)

type Provider struct {
	client *http.Client
}

func NewProvider() *Provider {
	return &Provider{client: &http.Client{Timeout: 10 * time.Second}}
}

func (p *Provider) FetchUSDKZT(ctx context.Context) (fx.SourceRate, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://nationalbank.kz/rss/rates_all.xml", nil)
	if err != nil {
		return fx.SourceRate{}, err
	}

	response, err := p.client.Do(request)
	if err != nil {
		return fx.SourceRate{}, err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	if response.StatusCode != http.StatusOK {
		return fx.SourceRate{}, fmt.Errorf("NBK returned HTTP %d", response.StatusCode)
	}

	var feed struct {
		Items []struct {
			Currency string `xml:"title"`
			Date     string `xml:"pubDate"`
			Rate     string `xml:"description"`
			Quantity int    `xml:"quant"`
		} `xml:"channel>item"`
	}
	if errDecoder := xml.NewDecoder(io.LimitReader(response.Body, 512<<10)).Decode(&feed); errDecoder != nil {
		return fx.SourceRate{}, errDecoder
	}

	for _, item := range feed.Items {
		if item.Currency != "USD" || item.Quantity != 1 {
			continue
		}

		date, errParse := time.ParseInLocation("02.01.2006", item.Date, almaty)
		rate, ok := decimal.Parse(item.Rate)
		if errParse == nil && ok && rate.Sign() > 0 {
			return fx.SourceRate{
				Provider:      "nbk",
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				Rate:          item.Rate,
				PricedAt:      date.UTC(),
				Source:        "official",
			}, nil
		}
	}

	return fx.SourceRate{}, fmt.Errorf("NBK feed did not provide a valid USD/KZT rate")
}
