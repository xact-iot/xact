package traffic_images_driver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// FetchTrafficImages retrieves traffic camera images from the LTA API
func (c *TrafficImagesClient) FetchTrafficImages() ([]TrafficImageData, error) {
	url := fmt.Sprintf("%s/Traffic-Imagesv2", c.baseURL)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Add("AccountKey", c.apiKey)
	req.Header.Add("accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch traffic images: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var imagesResp TrafficImagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&imagesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// fmt.Printf("[TrafficImages] Received %d camera records\n", len(imagesResp.Value))
	// for _, img := range imagesResp.Value {
	// 	fmt.Printf("[TrafficImages] Camera %-8s lat=%.6f lon=%.6f url=%s\n",
	// 		img.CameraID, img.Latitude, img.Longitude, img.ImageLink)
	// }

	return imagesResp.Value, nil
}
