package lta_driver

// VMSResponse represents the LTA API response for VMS data
type VMSResponse struct {
	Value []VMSData `json:"value"`
}

// VMSData represents a single Variable Message Sign
type VMSData struct {
	EquipmentID string  `json:"EquipmentID"`
	Latitude    float64 `json:"Latitude"`
	Longitude   float64 `json:"Longitude"`
	Message     string  `json:"Message"`
}

// VMSClient handles communication with the LTA DataMall API
type VMSClient struct {
	apiKey  string
	baseURL string
}

// NewVMSClient creates a new VMS client
func NewVMSClient(apiKey string) *VMSClient {
	return &VMSClient{
		apiKey:  apiKey,
		baseURL: "https://datamall2.mytransport.sg:443/ltaodataservice",
	}
}
