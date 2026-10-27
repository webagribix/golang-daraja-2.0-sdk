package mpesa

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Environment indicates the current mode the application is running on. Either EnvironmentSandbox or EnvironmentProduction.
type Environment uint8

// cache stores the AuthorizationResponse for the specified accessTokenTTL
type cache map[string]AuthorizationResponse

const (
	EnvironmentSandbox Environment = iota
	EnvironmentProduction
)

type ResponseType string

const (
	ResponseTypeCanceled ResponseType = "Canceled"
	ResponseTypeComplete ResponseType = "Completed"
)

var accessTokenTTL = 55 * time.Minute

// requiredURLScheme present the required scheme for the callbacks
const requiredURLScheme = "https"

const (
	sandboxBaseURL    = "https://sandbox.safaricom.co.ke"
	productionBaseURL = "https://api.safaricom.co.ke"
)

// IsProduction returns true if the current env is set to production.
func (e Environment) IsProduction() bool {
	return e == EnvironmentProduction
}

// BaseURL returns the base url for the current Environment
func (e Environment) BaseURL() string {
	if !e.IsProduction() {
		return sandboxBaseURL
	}

	return productionBaseURL
}

type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

//go:embed certs
var certFS embed.FS

// Mpesa is an app to make a transaction
type Mpesa struct {
	client         HttpClient
	environment    Environment
	mu             sync.Mutex
	cache          cache
	consumerKey    string
	consumerSecret string
	passkey        string
	initiatorPwd   string
	customCert     []byte
}

var (
	// ErrInvalidPasskey indicates that no passkey was provided.
	ErrInvalidPasskey = errors.New("mpesa: passkey cannot be empty")

	// ErrInvalidInitiatorPassword indicates that no initiator password was provided.
	ErrInvalidInitiatorPassword = errors.New("mpesa: initiator password cannot be empty")
)

// validateURL checks if the provided URL is valid and is being server via https
func validateURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("mpesa: %v", err)
	}

	if u.Scheme != requiredURLScheme {
		return fmt.Errorf("mpesa: %q must use %q", rawURL, requiredURLScheme)
	}

	return nil
}

// NormalizePhoneNumber formats any Kenyan phone number into the standard Safaricom Daraja format (2547XXXXXXXX or 2541XXXXXXXX).
// It accepts inputs like "+254712345678", "0712345678", "712345678", "0112345678", or "254712345678".
func NormalizePhoneNumber(phone string) (string, error) {
	var digits strings.Builder
	for _, r := range phone {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}

	cleaned := digits.String()
	if strings.HasPrefix(cleaned, "0") && len(cleaned) == 10 {
		cleaned = "254" + cleaned[1:]
	} else if (strings.HasPrefix(cleaned, "7") || strings.HasPrefix(cleaned, "1")) && len(cleaned) == 9 {
		cleaned = "254" + cleaned
	}

	if len(cleaned) != 12 || !strings.HasPrefix(cleaned, "254") {
		return "", fmt.Errorf("mpesa: invalid phone number %q, expected valid Kenyan phone number (e.g. 07XXXXXXXX, 01XXXXXXXX, or 2547XXXXXXXX)", phone)
	}

	return cleaned, nil
}

// NormalizePhoneNumberUint64 normalizes a phone string and returns it as a uint64 ready for SDK requests.
func NormalizePhoneNumberUint64(phone string) (uint64, error) {
	normalized, err := NormalizePhoneNumber(phone)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(normalized, 10, 64)
}

// Config holds configuration options for initializing the Daraja SDK client.
type Config struct {
	// ConsumerKey is the Daraja App Consumer Key (Required)
	ConsumerKey string

	// ConsumerSecret is the Daraja App Consumer Secret (Required)
	ConsumerSecret string

	// Passkey is the Lipa Na M-Pesa Online passkey (Optional default for STK Push and STK Query)
	Passkey string

	// InitiatorPassword is the password for B2C/Balance/Reversals (Optional default)
	InitiatorPassword string

	// Environment is either EnvironmentSandbox (default) or EnvironmentProduction
	Environment Environment

	// HTTPClient is an optional custom HTTP client
	HTTPClient HttpClient

	// CertificatePEM is an optional custom Safaricom public certificate in PEM format
	CertificatePEM []byte

	// CertificateFile is an optional path to a .cer / .pem certificate file
	CertificateFile string
}

// New creates and initializes a new M-Pesa Daraja client using the provided Config struct.
func New(cfg Config) (*Mpesa, error) {
	if cfg.ConsumerKey == "" || cfg.ConsumerSecret == "" {
		return nil, errors.New("mpesa: ConsumerKey and ConsumerSecret are required in Config")
	}

	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	app := &Mpesa{
		client:         c,
		environment:    cfg.Environment,
		cache:          make(cache),
		consumerKey:    cfg.ConsumerKey,
		consumerSecret: cfg.ConsumerSecret,
		passkey:        cfg.Passkey,
		initiatorPwd:   cfg.InitiatorPassword,
		customCert:     cfg.CertificatePEM,
	}

	if cfg.CertificateFile != "" {
		if err := app.WithCertificateFile(cfg.CertificateFile); err != nil {
			return nil, err
		}
	}

	return app, nil
}

// WithPasskey sets a default passkey for STK Push and STK Query operations.
func (m *Mpesa) WithPasskey(passkey string) *Mpesa {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.passkey = passkey
	return m
}

// WithInitiatorPassword sets a default initiator password for B2C, Balance, and Reversal operations.
func (m *Mpesa) WithInitiatorPassword(pwd string) *Mpesa {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initiatorPwd = pwd
	return m
}

// NewApp initializes a new Mpesa app that will be used to perform C2B or B2C transactions.
func NewApp(c HttpClient, consumerKey, consumerSecret string, env Environment) *Mpesa {
	if c == nil {
		c = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	return &Mpesa{
		client:      c,
		environment: env,
		cache:       make(cache),

		consumerKey:    consumerKey,
		consumerSecret: consumerSecret,
	}
}

// WithCertificatePEM sets a custom Safaricom public certificate in PEM format for encrypting initiator passwords.
// If set, this overrides the embedded certificate for the active environment.
func (m *Mpesa) WithCertificatePEM(certPEM []byte) *Mpesa {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customCert = certPEM
	return m
}

// WithCertificateFile loads a custom Safaricom public certificate from a file path.
func (m *Mpesa) WithCertificateFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("mpesa: read certificate file: %w", err)
	}
	m.WithCertificatePEM(data)
	return nil
}

// endpointAuth returns the auth endpoint prefixed with the current Environment base URL
func (m *Mpesa) endpointAuth() string {
	return m.Environment().BaseURL() + `/oauth/v1/generate?grant_type=client_credentials`
}

// endpointAccountBalance returns the account balance endpoint prefixed with the current Environment base URL
func (m *Mpesa) endpointAccountBalance() string {
	return m.Environment().BaseURL() + `/mpesa/accountbalance/v1/query`
}

// endpointB2C returns the B2C endpoint prefixed with the current Environment base URL
func (m *Mpesa) endpointB2C() string {
	return m.Environment().BaseURL() + `/mpesa/b2c/v1/paymentrequest`
}

// endpointBusinessPayBill returns the Business Pay Bill endpoint prefixed with the current Environment base URL
func (m *Mpesa) endpointBusinessPayBill() string {
	return m.Environment().BaseURL() + `/mpesa/b2b/v1/paymentrequest`
}

// endpointC2BRegister returns the endpoint to register C2B callbacks prefixed with the current Environment base URL
func (m *Mpesa) endpointC2BRegister() string {
	return m.Environment().BaseURL() + `/mpesa/c2b/v1/registerurl`
}

// endpointDynamicQR returns the endpoint to generate dynamic QR code prefixed with the current Environment base URL
func (m *Mpesa) endpointDynamicQR() string {
	return m.Environment().BaseURL() + `/mpesa/qrcode/v1/generate`
}

// endpointSTK returns the endpoint to generate an STK push prefixed with the current Environment base URL
func (m *Mpesa) endpointSTK() string {
	return m.Environment().BaseURL() + `/mpesa/stkpush/v1/processrequest`
}

// endpointSTKQuery returns the endpoint to query the status of an STK request prefixed with the current Environment base URL
func (m *Mpesa) endpointSTKQuery() string {
	return m.Environment().BaseURL() + `/mpesa/stkpushquery/v1/query`
}

// endpointTransactionStatus returns the endpoint to query the status of a transaction prefixed with the current Environment base URL
func (m *Mpesa) endpointTransactionStatus() string {
	return m.Environment().BaseURL() + `/mpesa/transactionstatus/v1/query`
}

// endpointReversal returns the endpoint to request a transaction reversal prefixed with the current Environment base URL
func (m *Mpesa) endpointReversal() string {
	return m.Environment().BaseURL() + `/mpesa/reversal/v1/request`
}

// endpointTaxRemittance returns the endpoint to remit tax to KRA prefixed with the current Environment base URL
func (m *Mpesa) endpointTaxRemittance() string {
	return m.Environment().BaseURL() + `/mpesa/b2b/v1/remittax`
}

// generateTimestampAndPassword returns the current timestamp in the format YYYYMMDDHHmmss and a base64 encoded
// password in the format shortcode+passkey+timestamp
func generateTimestampAndPassword(shortcode uint, passkey string) (string, string) {
	timestamp := time.Now().Format("20060102150405")
	password := fmt.Sprintf("%d%s%s", shortcode, passkey, timestamp)
	return timestamp, base64.StdEncoding.EncodeToString([]byte(password))
}

// makeHttpRequestWithToken makes an API call to the provided url using the provided http method.
func (m *Mpesa) makeHttpRequestWithToken(
	ctx context.Context, method, url string, body interface{},
) (*http.Response, error) {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("mpesa: marshal request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("mpesa: create request: %v", err)
	}

	accessToken, err := m.GenerateAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", `Bearer `+accessToken)

	res, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mpesa: make request: %v", err)
	}

	return res, nil
}

// Environment returns the current environment the app is running on.
func (m *Mpesa) Environment() Environment {
	return m.environment
}

// GenerateAccessToken returns a time bound access token to call allowed APIs.
// This token should be used in all other subsequent responses to the APIs
// GenerateAccessToken will also cache the access token for the specified refresh after period
func (m *Mpesa) GenerateAccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cachedData, ok := m.cache[m.consumerKey]; ok {
		if cachedData.setAt.Add(accessTokenTTL).After(time.Now()) {
			return cachedData.AccessToken, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.endpointAuth(), nil)
	if err != nil {
		return "", fmt.Errorf("mpesa: create auth request: %v", err)
	}

	req.SetBasicAuth(m.consumerKey, m.consumerSecret)

	res, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mpesa: make auth request: %v", err)
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mpesa: auth failed with status: %v", res.Status)
	}

	var response AuthorizationResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("mpesa: decode auth response: %v", err)
	}

	response.setAt = time.Now()
	m.cache[m.consumerKey] = response
	return m.cache[m.consumerKey].AccessToken, nil
}

// STKPush initiates online payment on behalf of a customer using STKPush.
// If passkey is empty, the default passkey from Config or WithPasskey is used.
func (m *Mpesa) STKPush(ctx context.Context, passkey string, req STKPushRequest) (*Response, error) {
	if passkey == "" {
		passkey = m.passkey
	}
	if passkey == "" {
		return nil, ErrInvalidPasskey
	}

	req.Timestamp, req.Password = generateTimestampAndPassword(req.BusinessShortCode, passkey)

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointSTK(), req)
	if err != nil {
		return nil, err
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	return decodeResponse(res)
}

// UnmarshalSTKPushCallback decodes the provided value to STKPushCallback.
func UnmarshalSTKPushCallback(r io.Reader) (*STKPushCallback, error) {
	var callback STKPushCallback
	if err := json.NewDecoder(r).Decode(&callback); err != nil {
		return nil, fmt.Errorf("mpesa: decode: %v", err)
	}

	return &callback, nil
}

func (m *Mpesa) generateSecurityCredentials(initiatorPwd string) (string, error) {
	var (
		publicKey []byte
		err       error
	)

	m.mu.Lock()
	customCert := m.customCert
	m.mu.Unlock()

	if len(customCert) > 0 {
		publicKey = customCert
	} else {
		certPath := "certs/sandbox.cer"
		if m.Environment().IsProduction() {
			certPath = "certs/production.cer"
		}

		publicKey, err = certFS.ReadFile(certPath)
		if err != nil {
			return "", fmt.Errorf("mpesa: read cert: %v", err)
		}
	}

	block, _ := pem.Decode(publicKey)
	if block == nil {
		return "", errors.New("mpesa: failed to decode PEM block containing certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("mpesa: parse cert: %v", err)
	}

	rsaPublicKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("mpesa: certificate public key is not an RSA public key")
	}

	reader := rand.Reader
	signature, err := rsa.EncryptPKCS1v15(reader, rsaPublicKey, []byte(initiatorPwd))
	if err != nil {
		return "", fmt.Errorf("mpesa: encrypt password: %v", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

// B2C transacts between an M-Pesa short code to a phone number registered on M-Pesa
func (m *Mpesa) B2C(ctx context.Context, initiatorPwd string, req B2CRequest) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointB2C(), req)
	if err != nil {
		return nil, err
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	return decodeResponse(res)
}

// UnmarshalCallback decodes the provided value to Callback
func UnmarshalCallback(r io.Reader) (*Callback, error) {
	var callback Callback
	if err := json.NewDecoder(r).Decode(&callback); err != nil {
		return nil, fmt.Errorf("mpesa: decode: %v", err)
	}

	return &callback, nil
}

// STKQuery checks the status of an STKPush payment.
// If passkey is empty, the default passkey from Config or WithPasskey is used.
func (m *Mpesa) STKQuery(ctx context.Context, passkey string, req STKQueryRequest) (*Response, error) {
	if passkey == "" {
		passkey = m.passkey
	}
	if passkey == "" {
		return nil, ErrInvalidPasskey
	}

	req.Timestamp, req.Password = generateTimestampAndPassword(req.BusinessShortCode, passkey)

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointSTKQuery(), req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return decodeResponse(res)
}

// PollSTKStatus polls Safaricom Daraja STK Query until the user enters their PIN, cancels, or the timeout expires.
// Default timeout is 45s, default interval is 3s. If passkey is empty, the default passkey from Config is used.
func (m *Mpesa) PollSTKStatus(ctx context.Context, passkey string, shortcode uint, checkoutRequestID string, timeout, interval time.Duration) (*Response, error) {
	if passkey == "" {
		passkey = m.passkey
	}
	if passkey == "" {
		return nil, ErrInvalidPasskey
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	if interval <= 0 {
		interval = 3 * time.Second
	}

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return nil, fmt.Errorf("mpesa: timeout polling STK status for CheckoutRequestID %s: %w", checkoutRequestID, pollCtx.Err())
		case <-ticker.C:
			res, err := m.STKQuery(pollCtx, passkey, STKQueryRequest{
				BusinessShortCode: shortcode,
				CheckoutRequestID: checkoutRequestID,
			})
			if err != nil {
				var apiErr *APIError
				if errors.As(err, &apiErr) {
					// 500.001.1001 indicates "The transaction is being processed", continue polling
					if apiErr.ErrorCode == "500.001.1001" || strings.Contains(strings.ToLower(apiErr.ErrorMessage), "being processed") {
						continue
					}
				}
				continue
			}

			// When ResultCode is present, Safaricom has completed processing!
			if res.ResultCode != "" {
				return res, nil
			}
		}
	}
}

// RegisterC2BURL API works hand in hand with Customer to Business (C2B) APIs and allows receiving payment notifications to your paybill.
// This API enables you to register the callback URLs via which you shall receive notifications for payments to your pay bill/till number.
// There are two URLs required for Register URL API: Validation URL and Confirmation URL.
// Validation URL: This is the URL that is only used when a Merchant (Partner) requires to validate the details of the payment before accepting.
// For example, a bank would want to verify if an account number exists in their platform before accepting a payment from the customer.
// Confirmation URL:  This is the URL that receives payment notification once payment has been completed successfully on M-PESA.
func (m *Mpesa) RegisterC2BURL(ctx context.Context, req RegisterC2BURLRequest) (*Response, error) {
	switch req.ResponseType {
	case ResponseTypeComplete, ResponseTypeCanceled:
		response, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointC2BRegister(), req)
		if err != nil {
			return nil, err
		}
		defer func(body io.ReadCloser) {
			_ = body.Close()
		}(response.Body)

		return decodeResponse(response)
	default:
		return nil, fmt.Errorf("mpesa: the provided ResponseType [%s] is not valid", req.ResponseType)
	}
}

// DynamicQR API is used to generate a Dynamic QR which enables Safaricom M-PESA customers who have My Safaricom App or
// M-PESA app, to scan a QR (Quick Response) code, to capture till number and amount then authorize to pay for goods and
// services at select LIPA NA M-PESA (LNM) merchant outlets. If the decodeImage parameter is set to true, the QR code
// will be decoded and a base url is set on the ImagePath field
func (m *Mpesa) DynamicQR(
	ctx context.Context, req DynamicQRRequest, transactionType DynamicQRTransactionType, decodeImage bool,
) (*DynamicQRResponse, error) {
	req.TransactionType = transactionType

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointDynamicQR(), req)
	if err != nil {
		return nil, err
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	var resp *DynamicQRResponse
	if err = json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("mpesa: decode response: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"mpesa: request %v failed with code %v: %v", resp.RequestID, resp.ErrorCode, resp.ErrorMessage,
		)
	}

	if !decodeImage {
		return resp, nil
	}

	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(resp.QRCode))

	image, err := png.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("mpesa: decode png: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("mpesa: wd: %v", err)
	}

	imagesDir := filepath.Join(wd, "storage", "images")
	if _, err := os.Stat(imagesDir); os.IsNotExist(err) {
		if err = os.Mkdir(imagesDir, os.ModePerm); err != nil {
			return nil, fmt.Errorf("mpesa: create images dir: %v", err)
		}
	}

	amountStr := strconv.Itoa(int(req.Amount))
	filename := req.MerchantName + "_" + amountStr + "_" + req.CreditPartyIdentifier + ".png"
	filename = filepath.Join(imagesDir, strings.ReplaceAll(filename, " ", "_"))

	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("mpesa: open png: %v", err)
	}
	defer f.Close()

	if err = png.Encode(f, image); err != nil {
		return nil, fmt.Errorf("mpesa: encode png: %v", err)
	}

	resp.ImagePath = filename
	return resp, nil
}

// GetTransactionStatus checks the status of a transaction
func (m *Mpesa) GetTransactionStatus(
	ctx context.Context, initiatorPwd string, req TransactionStatusRequest,
) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	if err := validateURL(req.QueueTimeOutURL); err != nil {
		return nil, err
	}

	if err := validateURL(req.ResultURL); err != nil {
		return nil, err
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential
	req.CommandID = TransactionStatusQueryCommandID
	req.IdentifierType = ShortcodeIdentifierType

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointTransactionStatus(), req)
	if err != nil {
		return nil, err
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	return decodeResponse(res)
}

// GetAccountBalance fetches the account balance of a short code. This can be used for both B2C, buy goods and pay bill
// accounts.
func (m *Mpesa) GetAccountBalance(
	ctx context.Context, initiatorPwd string, req AccountBalanceRequest,
) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	if err := validateURL(req.QueueTimeOutURL); err != nil {
		return nil, err
	}

	if err := validateURL(req.ResultURL); err != nil {
		return nil, err
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential
	req.CommandID = AccountBalanceCommandID
	req.IdentifierType = ShortcodeIdentifierType

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointAccountBalance(), req)
	if err != nil {
		return nil, err
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	return decodeResponse(res)
}

// BusinessPayBill API enables you to pay bills directly from your business account to a pay bill number, or a paybill
// store. You can use this API to pay on behalf of a consumer/requester.
//
// The transaction moves money from your MMF/Working account to the recipient’s utility account.
func (m *Mpesa) BusinessPayBill(ctx context.Context, initiatorPwd string, req BusinessPayBillRequest) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	if err := validateURL(req.QueueTimeOutURL); err != nil {
		return nil, err
	}

	if err := validateURL(req.ResultURL); err != nil {
		return nil, err
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential
	req.CommandID = BusinessPayBillCommandID
	req.RecieverIdentifierType = ShortcodeIdentifierType
	req.SenderIdentifierType = ShortcodeIdentifierType

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointBusinessPayBill(), req)
	if err != nil {
		return nil, err
	}

	//goland:noinspection GoUnhandledErrorResult
	defer res.Body.Close()

	return decodeResponse(res)
}

// Reversal requests a reversal of an M-Pesa transaction.
func (m *Mpesa) Reversal(ctx context.Context, initiatorPwd string, req ReversalRequest) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	if err := validateURL(req.QueueTimeOutURL); err != nil {
		return nil, err
	}

	if err := validateURL(req.ResultURL); err != nil {
		return nil, err
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential
	req.CommandID = TransactionReversalCommandID
	if req.RecieverIdentifierType == 0 {
		req.RecieverIdentifierType = ShortcodeIdentifierType
	}

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointReversal(), req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return decodeResponse(res)
}

// TaxRemittance enables businesses to remit tax directly to the Kenya Revenue Authority (KRA).
func (m *Mpesa) TaxRemittance(ctx context.Context, initiatorPwd string, req TaxRemittanceRequest) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	if err := validateURL(req.QueueTimeOutURL); err != nil {
		return nil, err
	}

	if err := validateURL(req.ResultURL); err != nil {
		return nil, err
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential
	req.CommandID = PayTaxToKRACommandID
	req.SenderIdentifierType = ShortcodeIdentifierType
	req.RecieverIdentifierType = ShortcodeIdentifierType

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointTaxRemittance(), req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return decodeResponse(res)
}

// BusinessBuyGoods enables a business to pay directly from its account to a Merchant Buy Goods till (B2B).
func (m *Mpesa) BusinessBuyGoods(ctx context.Context, initiatorPwd string, req BusinessBuyGoodsRequest) (*Response, error) {
	if initiatorPwd == "" {
		initiatorPwd = m.initiatorPwd
	}
	if initiatorPwd == "" {
		return nil, ErrInvalidInitiatorPassword
	}

	if err := validateURL(req.QueueTimeOutURL); err != nil {
		return nil, err
	}

	if err := validateURL(req.ResultURL); err != nil {
		return nil, err
	}

	securityCredential, err := m.generateSecurityCredentials(initiatorPwd)
	if err != nil {
		return nil, err
	}

	req.SecurityCredential = securityCredential
	req.CommandID = BusinessBuyGoodsCommandID
	req.RecieverIdentifierType = ShortcodeIdentifierType
	req.SenderIdentifierType = ShortcodeIdentifierType

	res, err := m.makeHttpRequestWithToken(ctx, http.MethodPost, m.endpointBusinessPayBill(), req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return decodeResponse(res)
}

// APIError represents an error returned by the Safaricom M-Pesa Daraja API.
type APIError struct {
	StatusCode   int    `json:"-"`
	RequestID    string `json:"requestId,omitempty"`
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

func (e *APIError) Error() string {
	if e.ErrorCode != "" {
		return fmt.Sprintf("mpesa: request %v failed with code %v: %v", e.RequestID, e.ErrorCode, e.ErrorMessage)
	}
	return fmt.Sprintf("mpesa: request failed with status %d: %v", e.StatusCode, e.ErrorMessage)
}

func decodeResponse(res *http.Response) (*Response, error) {
	var resp Response
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("mpesa: decode response: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode:   res.StatusCode,
			RequestID:    resp.RequestID,
			ErrorCode:    resp.ErrorCode,
			ErrorMessage: resp.ErrorMessage,
		}
	}

	return &resp, nil
}
