package connect

import (
	"testing"
)

func TestClientInfoUnknownCaseCannotOverrideCanonicalFields(t *testing.T) {
	value := ParseClientInfo(`{"v":1,"V":99,"device_type":"ios","DeviceType":"injected","app_version":"1","APP_VERSION":"bad"}`, "")
	if value.Version != 1 || value.DeviceType != "ios" || value.AppVersion != "1" {
		t.Fatal(value)
	}
}
