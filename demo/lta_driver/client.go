package lta_driver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FetchVMSData retrieves VMS data from the LTA API
func (c *VMSClient) FetchVMSData() ([]VMSData, error) {
	url := fmt.Sprintf("%s/VMS", c.baseURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add required headers
	req.Header.Add("AccountKey", c.apiKey)
	req.Header.Add("accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch VMS data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	var vmsResp VMSResponse
	if err := json.NewDecoder(resp.Body).Decode(&vmsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	// fmt.Printf("%+v\n", vmsResp.Value)

	return vmsResp.Value, nil
}
