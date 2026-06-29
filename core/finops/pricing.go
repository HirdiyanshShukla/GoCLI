package finops

// Hardcoded generic on-demand rates. NOT provider/region accurate; this is
// a rough estimate, always disclosed as such in terminal output.
var regionRates = map[string]struct {
	CPUHourly float64 // per vCPU-hour
	MemHourly float64 // per GB-hour
}{
	"default":         {CPUHourly: 0.0416, MemHourly: 0.0041},
	"aws-us-east-1":   {CPUHourly: 0.0416, MemHourly: 0.0041},
	"gcp-us-central1": {CPUHourly: 0.0400, MemHourly: 0.0040},
}

const hoursPerMonth = 24 * 30

// ResourceTotals holds summed CPU (millicores) and memory (MiB) across all
// containers in a deployment, before multiplying by replica count.
type ResourceTotals struct {
	CPUMillicores int64
	MemoryMiB     int64
	Replicas      int
}

// MonthlyCost computes a deterministic monthly estimate. The AI never
// computes this; it only ever suggests new ResourceTotals, which get
// re-fed through this same function.
func MonthlyCost(totals ResourceTotals, region string) float64 {
	rates, ok := regionRates[region]
	if !ok {
		rates = regionRates["default"]
	}
	vcpu := float64(totals.CPUMillicores) / 1000.0
	gbMem := float64(totals.MemoryMiB) / 1024.0
	perReplica := (vcpu * rates.CPUHourly) + (gbMem * rates.MemHourly)
	return perReplica * hoursPerMonth * float64(totals.Replicas)
}
