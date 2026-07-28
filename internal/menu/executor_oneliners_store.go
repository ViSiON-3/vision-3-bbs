package menu

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

func loadOnelinerRecords(onelinerPath string) ([]onelinerRecord, error) {
	jsonData, readErr := os.ReadFile(onelinerPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return []onelinerRecord{}, nil
		}
		return nil, readErr
	}

	if strings.TrimSpace(string(jsonData)) == "" {
		return []onelinerRecord{}, nil
	}

	var rawEntries []json.RawMessage
	if err := json.Unmarshal(jsonData, &rawEntries); err != nil {
		return nil, err
	}

	records := make([]onelinerRecord, 0, len(rawEntries))
	for _, raw := range rawEntries {
		var legacyText string
		if err := json.Unmarshal(raw, &legacyText); err == nil {
			legacyText = truncateOnelinerPreservePipeCodes(legacyText, oneLinerMaxLength)
			if legacyText != "" {
				records = append(records, onelinerRecord{
					Text:             legacyText,
					PostedByUsername: "Unknown",
				})
			}
			continue
		}

		var compat onelinerRecordCompat
		if err := json.Unmarshal(raw, &compat); err != nil {
			continue
		}

		record := onelinerRecord{
			Text:             truncateOnelinerPreservePipeCodes(compat.Text, oneLinerMaxLength),
			Anonymous:        compat.Anonymous,
			PostedByUsername: strings.TrimSpace(compat.PostedByUsername),
			PostedByHandle:   strings.TrimSpace(compat.PostedByHandle),
			PostedAt:         compat.PostedAt,
		}

		if record.PostedByUsername == "" {
			record.PostedByUsername = strings.TrimSpace(compat.Username)
		}
		if record.PostedByHandle == "" {
			if strings.TrimSpace(compat.DisplayName) != "" && !record.Anonymous {
				record.PostedByHandle = strings.TrimSpace(compat.DisplayName)
			} else if strings.TrimSpace(compat.Username) != "" {
				record.PostedByHandle = strings.TrimSpace(compat.Username)
			}
		}

		if record.Text == "" {
			continue
		}

		records = append(records, record)
	}

	return records, nil
}

func saveOnelinerRecords(onelinerPath string, records []onelinerRecord) error {
	if len(records) > oneLinerMaxStored {
		records = records[len(records)-oneLinerMaxStored:]
	}

	updatedJSON, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(onelinerPath, updatedJSON, 0644)
}

// Mutex for protecting access to the oneliners file
var onelinerMutex sync.Mutex

type onelinerRecord struct {
	Text             string `json:"text"`
	Anonymous        bool   `json:"anonymous,omitempty"`
	PostedByUsername string `json:"posted_by_username,omitempty"`
	PostedByHandle   string `json:"posted_by_handle,omitempty"`
	PostedAt         string `json:"posted_at,omitempty"`
}

type onelinerRecordCompat struct {
	DisplayName      string `json:"display_name,omitempty"`
	Username         string `json:"username,omitempty"`
	Text             string `json:"text"`
	Anonymous        bool   `json:"anonymous,omitempty"`
	PostedByUsername string `json:"posted_by_username,omitempty"`
	PostedByHandle   string `json:"posted_by_handle,omitempty"`
	PostedAt         string `json:"posted_at,omitempty"`
}
