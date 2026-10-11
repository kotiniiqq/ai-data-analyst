// Package cost estimates how much a query would scan before it runs, so an
// expensive query can wait for a person to approve it.
package cost

import "context"

// USDPerTiB is the on-demand price used for estimates.
const USDPerTiB = 6.25

// DefaultApprovalBytes is the default threshold: 100 MiB.
const DefaultApprovalBytes = 100 << 20

type Estimate struct {
	Bytes         int64   `json:"bytes"`
	USD           float64 `json:"usd"`
	NeedsApproval bool    `json:"needs_approval"`
	Source        string  `json:"source"`
}

type Estimator interface {
	Estimate(ctx context.Context, sql string) (Estimate, error)
}

// FromBytes prices b bytes; at or above threshold the query needs approval.
func FromBytes(b, threshold int64) Estimate {
	return Estimate{Bytes: b, USD: float64(b) / (1 << 40) * USDPerTiB, NeedsApproval: b >= threshold}
}
