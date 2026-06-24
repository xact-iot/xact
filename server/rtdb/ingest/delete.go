package ingest

import (
	"fmt"

	"github.com/xact-iot/xact/rtdb/tree"
)

type DeviceDeleter interface {
	DeleteNode(path string) error
}

// DevicePath returns the RTDB path for an ingest device address.
func DevicePath(tenant, zone, deviceType, deviceName string) string {
	if zone != "" {
		return tenant + "." + zone + "." + deviceType + "." + deviceName
	}
	return tenant + "." + deviceType + "." + deviceName
}

// DeleteDevice removes a device and all child tags using the same address
// components used by REST, MQTT, and NATS ingest.
func DeleteDevice(deleter DeviceDeleter, tenant, zone, deviceType, deviceName string) error {
	if deleter == nil {
		return fmt.Errorf("device delete unavailable")
	}
	if tenant == "" || deviceType == "" || deviceName == "" {
		return fmt.Errorf("tenant, deviceType, and deviceName are required")
	}
	return deleter.DeleteNode(DevicePath(tenant, zone, deviceType, deviceName))
}

var _ DeviceDeleter = (*tree.TreeWithOperations)(nil)
