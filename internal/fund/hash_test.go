package fund

import (
	"testing"
	"time"
)

func TestSnapshotSourceHashIgnoresSourceRowOrder(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot()
	hash1, err := SnapshotSourceHash(snapshot)
	if err != nil {
		t.Fatalf("SnapshotSourceHash() error = %v", err)
	}

	snapshot.Assets[0], snapshot.Assets[1] = snapshot.Assets[1], snapshot.Assets[0]
	snapshot.Categories[0], snapshot.Categories[1] = snapshot.Categories[1], snapshot.Categories[0]

	hash2, err := SnapshotSourceHash(snapshot)
	if err != nil {
		t.Fatalf("SnapshotSourceHash() reordered error = %v", err)
	}

	if hash1 != hash2 {
		t.Fatalf("hash changed after row reorder: %s != %s", hash1, hash2)
	}
}

func TestSnapshotSourceHashNormalizesDecimals(t *testing.T) {
	t.Parallel()

	left := testSnapshot()
	right := testSnapshot()

	right.CalculatedUnitValueUSD = "031.1800"
	right.NAVUSD = "492986650.000"
	right.Assets[0].Quantity = "0584986.0"
	right.Assets[0].AssetSharePercent = "8.7600"
	right.Categories[0].AssetSharePercent = "75.125400"

	leftHash, err := SnapshotSourceHash(left)
	if err != nil {
		t.Fatalf("left hash error = %v", err)
	}
	rightHash, err := SnapshotSourceHash(right)
	if err != nil {
		t.Fatalf("right hash error = %v", err)
	}

	if leftHash != rightHash {
		t.Fatalf("equivalent decimals produced different hashes: %s != %s", leftHash, rightHash)
	}
}

func TestSnapshotSourceHashChangesWithSourceFact(t *testing.T) {
	t.Parallel()

	left := testSnapshot()
	right := testSnapshot()
	right.Assets[0].Quantity = "584987"

	leftHash, err := SnapshotSourceHash(left)
	if err != nil {
		t.Fatalf("left hash error = %v", err)
	}
	rightHash, err := SnapshotSourceHash(right)
	if err != nil {
		t.Fatalf("right hash error = %v", err)
	}

	if leftHash == rightHash {
		t.Fatalf("hash did not change after quantity change: %s", leftHash)
	}
}

func testSnapshot() SourceSnapshot {
	return SourceSnapshot{
		AsOfDate:               time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC),
		CalculatedUnitValueUSD: "31.18",
		NAVUSD:                 "492986650",
		DeclaredAssetCount:     2,
		Assets: []SourceAsset{
			{
				SourceName:        "АО НК КазМунайГаз",
				SourceType:        "Акции",
				Kind:              AssetKindEquity,
				ISIN:              "KZ1C00001122",
				Quantity:          "584986",
				AssetSharePercent: "8.76",
			},
			{
				SourceName:           "ООО \"ЦИФРА Банк\" (USD)",
				SourceType:           "Денежные средства в кредитной организации",
				Kind:                 AssetKindBankCash,
				Currency:             "USD",
				AssetSharePercent:    "0.01",
				AssetShareUpperBound: true,
			},
		},
		Categories: []SourceCategory{
			{SourceName: "Облигации", AssetSharePercent: "75.1254"},
			{SourceName: "Акции", AssetSharePercent: "18.6589"},
		},
	}
}
