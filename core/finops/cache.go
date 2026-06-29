package finops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const cacheFileName = ".devsandbox-finops-cache.json"

// Bump this number anytime you change the CacheEntry struct.
const cacheSchemaVersion = 2

// CachedMutation mirrors ai.FinOpsMutation but lives in the finops package
// to avoid an import cycle.
type CachedMutation struct {
	ContainerName string `json:"container_name"`
	FieldPath     string `json:"field_path"`
	OldValue      string `json:"old_value"`
	NewValue      string `json:"new_value"`
	Reasoning     string `json:"reasoning"`
	ChangeType    string `json:"change_type"`
}

// CacheEntry stores the resource fingerprint and computed cost from the last
// validate run so we can skip the AI call when nothing has changed.
type CacheEntry struct {
	Version     int              `json:"version"`
	Hash        string           `json:"hash"`
	CurrentCost float64          `json:"current_cost"`
	Mutations   []CachedMutation `json:"mutations,omitempty"`
}

// HashTotals produces a stable fingerprint of the resource totals.
func HashTotals(totals ResourceTotals) string {
	data := fmt.Sprintf("%d:%d:%d", totals.CPUMillicores, totals.MemoryMiB, totals.Replicas)
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// LoadCache reads the finops cache file from the project root.
// Returns false if the file does not exist, cannot be parsed, or has a stale
// schema version so that old cache files never silently break the app.
func LoadCache(projectPath string) (CacheEntry, bool) {
	var entry CacheEntry
	data, err := os.ReadFile(filepath.Join(projectPath, cacheFileName))
	if err != nil {
		return entry, false
	}
	if json.Unmarshal(data, &entry) != nil {
		return entry, false
	}
	// THE FIX: If the version does not match, force a cache miss.
	if entry.Version != cacheSchemaVersion {
		return entry, false
	}
	return entry, true
}

// SaveCache writes the finops cache entry to the project root.
// Errors are silently ignored; a missing cache only causes an extra AI call.
func SaveCache(projectPath string, entry CacheEntry) {
	entry.Version = cacheSchemaVersion // Stamp the current version before saving
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(projectPath, cacheFileName), data, 0644)
}