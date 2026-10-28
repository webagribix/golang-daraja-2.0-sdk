package mpesa

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPValidator_DefaultSafaricomIPs(t *testing.T) {
	v := NewIPValidator(WithDefaultSafaricomIPs())

	// Test known official Safaricom production IPs
	safaricomIPs := []string{
		"196.201.214.200",
		"196.201.214.206",
		"196.201.214.207",
		"196.201.214.208",
		"196.201.213.114",
		"196.201.213.44",
		"196.201.213.41",
		"196.201.212.127",
		"196.201.212.129",
		"196.201.212.136",
		"196.201.212.138",
		"196.201.212.74",
		"196.201.212.69",
		"196.207.242.15",  // matches 196.207.242.0/24
		"196.201.214.205", // matches 196.201.214.200/28
	}

	for _, ip := range safaricomIPs {
		require.True(t, v.IsAllowedIP(ip), "expected %s to be allowed", ip)
		require.True(t, IsSafaricomIP(ip), "expected IsSafaricomIP(%s) to be true", ip)
		// With host:port
		require.True(t, v.IsAllowedIP(ip+":44321"), "expected %s:44321 to be allowed", ip)
	}

	// Unauthorized IPs
	unauthorizedIPs := []string{
		"1.1.1.1",
		"8.8.8.8",
		"196.201.215.1",
		"192.168.1.1",
		"10.0.0.1",
		"invalid-ip",
	}

	for _, ip := range unauthorizedIPs {
		require.False(t, v.IsAllowedIP(ip), "expected %s to be blocked", ip)
		require.False(t, IsSafaricomIP(ip), "expected IsSafaricomIP(%s) to be false", ip)
	}
}

func TestIPValidator_CustomRangesAndCIDRs(t *testing.T) {
	v := NewIPValidator(
		WithAllowedIPs(
			"10.0.0.0/24",
			"192.168.1.50-192.168.1.100",
			"172.16.0.5",
		),
	)

	// CIDR check
	require.True(t, v.IsAllowedIP("10.0.0.1"))
	require.True(t, v.IsAllowedIP("10.0.0.254"))
	require.False(t, v.IsAllowedIP("10.0.1.1"))

	// Range check (inclusive)
	require.True(t, v.IsAllowedIP("192.168.1.50"))
	require.True(t, v.IsAllowedIP("192.168.1.75"))
	require.True(t, v.IsAllowedIP("192.168.1.100"))
	require.False(t, v.IsAllowedIP("192.168.1.49"))
	require.False(t, v.IsAllowedIP("192.168.1.101"))

	// Single IP check
	require.True(t, v.IsAllowedIP("172.16.0.5"))
	require.False(t, v.IsAllowedIP("172.16.0.6"))

	// Standalone ValidateCallbackIP helper
	require.True(t, ValidateCallbackIP("10.0.0.5", "10.0.0.0/24"))
	require.False(t, ValidateCallbackIP("1.2.3.4", "10.0.0.0/24"))
}

func TestIPValidator_DynamicAddAndSet(t *testing.T) {
	v := NewIPValidator(WithAllowedIPs("10.0.0.1"))
	require.True(t, v.IsAllowedIP("10.0.0.1"))
	require.False(t, v.IsAllowedIP("10.0.0.2"))

	// AddAllowedIPs dynamically
	err := v.AddAllowedIPs("10.0.0.2", "192.168.0.0/16")
	require.NoError(t, err)
	require.True(t, v.IsAllowedIP("10.0.0.2"))
	require.True(t, v.IsAllowedIP("192.168.5.10"))

	// GetAllowedIPs
	entries := v.GetAllowedIPs()
	require.Contains(t, entries, "10.0.0.1")
	require.Contains(t, entries, "10.0.0.2")
	require.Contains(t, entries, "192.168.0.0/16")

	// SetAllowedIPs resets and replaces
	err = v.SetAllowedIPs("172.20.0.0/16")
	require.NoError(t, err)
	require.True(t, v.IsAllowedIP("172.20.1.1"))
	require.False(t, v.IsAllowedIP("10.0.0.1")) // old IP should now be blocked

	// Error on malformed range
	require.Error(t, v.AddAllowedIPs("invalid-range-format"))
	require.Error(t, v.AddAllowedIPs("10.0.0.1-invalid"))
	require.Error(t, v.AddAllowedIPs("10.0.0.100-10.0.0.50")) // start > end
}

func TestIPValidator_ProxyHeaderExtraction(t *testing.T) {
	v := NewIPValidator(
		WithAllowedIPs("196.201.214.200"),
		WithTrustProxyHeaders(true),
	)

	// Test X-Forwarded-For (single IP)
	req := httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "196.201.214.200")
	req.RemoteAddr = "10.0.0.1:54321" // ALB proxy IP
	require.Equal(t, "196.201.214.200", v.GetClientIP(req))
	require.True(t, v.IsAllowedIP(v.GetClientIP(req)))

	// Test X-Forwarded-For (multiple comma-separated IPs)
	req = httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "196.201.214.200, 10.0.0.2, 172.16.0.1")
	require.Equal(t, "196.201.214.200", v.GetClientIP(req))

	// Test CF-Connecting-IP
	req = httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("CF-Connecting-IP", "196.201.214.200")
	require.Equal(t, "196.201.214.200", v.GetClientIP(req))

	// When trustProxyHeaders is disabled, inspects RemoteAddr only
	vNoProxy := NewIPValidator(
		WithAllowedIPs("196.201.214.200"),
		WithTrustProxyHeaders(false),
	)
	req = httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "196.201.214.200")
	req.RemoteAddr = "192.168.1.1:12345"
	require.Equal(t, "192.168.1.1", vNoProxy.GetClientIP(req))
	require.False(t, vNoProxy.IsAllowedIP(vNoProxy.GetClientIP(req)))
}

func TestIPValidator_HTTPMiddleware(t *testing.T) {
	v := NewIPValidator(WithAllowedIPs("196.201.214.200"))

	handlerCalled := false
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	middleware := v.Middleware(testHandler)

	// Authorized request
	req := httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "196.201.214.200")
	rr := httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)
	require.True(t, handlerCalled)
	require.Equal(t, http.StatusOK, rr.Code)

	// Unauthorized request
	handlerCalled = false
	req = httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rr = httptest.NewRecorder()
	middleware.ServeHTTP(rr, req)
	require.False(t, handlerCalled)
	require.Equal(t, http.StatusForbidden, rr.Code)
}

func TestIPValidator_Concurrency(t *testing.T) {
	v := NewIPValidator(WithDefaultSafaricomIPs())

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = v.IsAllowedIP("196.201.214.200")
			_ = v.IsAllowedIP("1.1.1.1")
		}()
		go func(idx int) {
			defer wg.Done()
			_ = v.AddAllowedIPs("10.0.0." + string(rune('0'+(idx%10))))
		}(i)
	}
	wg.Wait()
}

func TestMpesa_ClientIPValidatorIntegration(t *testing.T) {
	client, err := New(Config{
		ConsumerKey:        "test-key",
		ConsumerSecret:     "test-secret",
		AllowedCallbackIPs: []string{"10.50.0.0/16"},
	})
	require.NoError(t, err)

	// Default Safaricom IPs should be active
	require.True(t, client.IsAllowedCallbackIP("196.201.214.200"))
	// Custom configured range should be active
	require.True(t, client.IsAllowedCallbackIP("10.50.1.20"))
	// Unauthorized IP should be rejected
	require.False(t, client.IsAllowedCallbackIP("8.8.8.8"))

	// Dynamic modification on client
	err = client.AddAllowedCallbackIPs("172.16.1.1-172.16.1.10")
	require.NoError(t, err)
	require.True(t, client.IsAllowedCallbackIP("172.16.1.5"))

	// VerifyCallbackRequest helper
	req := httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "196.201.214.200")
	ip, err := client.VerifyCallbackRequest(req)
	require.NoError(t, err)
	require.Equal(t, "196.201.214.200", ip)

	// Unauthorized request
	req = httptest.NewRequest(http.MethodPost, "/callback", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	_, err = client.VerifyCallbackRequest(req)
	require.Error(t, err)
}

func TestSTKCallback_Helpers(t *testing.T) {
	sampleJSON := `{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "29115-34620561-1",
				"CheckoutRequestID": "ws_CO_191220191020363925",
				"ResultCode": 0,
				"ResultDesc": "The service request is processed successfully.",
				"CallbackMetadata": {
					"Item": [
						{"Name": "Amount", "Value": 10.00},
						{"Name": "MpesaReceiptNumber", "Value": "NLJ7RT61SV"},
						{"Name": "Balance"},
						{"Name": "TransactionDate", "Value": 20191219102115},
						{"Name": "PhoneNumber", "Value": 254708374149}
					]
				}
			}
		}
	}`

	cb, err := ParseSTKCallback(bytes.NewBufferString(sampleJSON))
	require.NoError(t, err)
	require.NotNil(t, cb)
	require.True(t, cb.IsSuccessful())
	require.False(t, cb.IsUserCancelled())
	require.False(t, cb.IsTimeout())
	require.Equal(t, "NLJ7RT61SV", cb.ReceiptNumber())

	amount, ok := cb.Amount()
	require.True(t, ok)
	require.Equal(t, 10.0, amount)

	phone, ok := cb.PhoneNumber()
	require.True(t, ok)
	require.Equal(t, uint64(254708374149), phone)

	require.Equal(t, "20191219102115", cb.TransactionDate())

	// Test user cancelled callback
	cancelledJSON := `{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "29115-34620561-1",
				"CheckoutRequestID": "ws_CO_191220191020363925",
				"ResultCode": 1032,
				"ResultDesc": "Request cancelled by user."
			}
		}
	}`
	cbCancelled, err := ParseSTKCallbackBytes([]byte(cancelledJSON))
	require.NoError(t, err)
	require.False(t, cbCancelled.IsSuccessful())
	require.True(t, cbCancelled.IsUserCancelled())
	require.False(t, cbCancelled.IsTimeout())

	// Test timeout callback
	timeoutJSON := `{
		"Body": {
			"stkCallback": {
				"MerchantRequestID": "29115-34620561-1",
				"CheckoutRequestID": "ws_CO_191220191020363925",
				"ResultCode": 1037,
				"ResultDesc": "DS timeout user cannot be reached"
			}
		}
	}`
	cbTimeout, err := ParseSTKCallbackBytes([]byte(timeoutJSON))
	require.NoError(t, err)
	require.False(t, cbTimeout.IsSuccessful())
	require.False(t, cbTimeout.IsUserCancelled())
	require.True(t, cbTimeout.IsTimeout())
}
