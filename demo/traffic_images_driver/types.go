package traffic_images_driver

// TrafficImagesResponse represents the LTA API response for traffic images
type TrafficImagesResponse struct {
	Value []TrafficImageData `json:"value"`
}

// TrafficImageData represents a single traffic camera image record
type TrafficImageData struct {
	CameraID  string  `json:"CameraID"`
	Latitude  float64 `json:"Latitude"`
	Longitude float64 `json:"Longitude"`
	ImageLink string  `json:"ImageLink"`
}

// TrafficImagesClient handles communication with the LTA DataMall API
type TrafficImagesClient struct {
	apiKey  string
	baseURL string
}

// NewTrafficImagesClient creates a new traffic images client
func NewTrafficImagesClient(apiKey string) *TrafficImagesClient {
	return &TrafficImagesClient{
		apiKey:  apiKey,
		baseURL: "https://datamall2.mytransport.sg:443/ltaodataservice",
	}
}
