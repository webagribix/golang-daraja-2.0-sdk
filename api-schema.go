package mpesa

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// DynamicQRTransactionType represents the supported transaction types for the Dynamic QR API.
type DynamicQRTransactionType string

const (
	PayMerchantBuyGoods      DynamicQRTransactionType = "BG"
	PaybillOrBusinessNumber  DynamicQRTransactionType = "PB"
	SendMoneyViaMobileNumber DynamicQRTransactionType = "SM"
	SentToBusiness           DynamicQRTransactionType = "SB"
	WithdrawCashAtAgentTill  DynamicQRTransactionType = "WA"
)

// CommandID is a unique command that specifies the transaction operation for B2C, B2B, or administrative requests.
type CommandID string

const (
	// AccountBalanceCommandID is applied when getting the account balance of a shortcode.
	AccountBalanceCommandID CommandID = "AccountBalance"

	// BusinessPayBillCommandID is applied for BusinessPayBillRequest (B2B paybill payment).
	BusinessPayBillCommandID CommandID = "BusinessPayBill"

	// BusinessPaymentCommandID is a normal business to customer payment, supports only M-PESA registered customers.
	BusinessPaymentCommandID CommandID = "BusinessPayment"

	// PromotionPaymentCommandID is a promotional payment to customers with a congratulatory message.
	PromotionPaymentCommandID CommandID = "PromotionPayment"

	// SalaryPaymentCommandID is used for sending salary to both registered and unregistered M-Pesa customers.
	SalaryPaymentCommandID CommandID = "SalaryPayment"

	// TransactionStatusQueryCommandID is applied when getting the status of a transaction.
	TransactionStatusQueryCommandID CommandID = "TransactionStatusQuery"

	// TransactionReversalCommandID is applied when reversing an M-Pesa transaction.
	TransactionReversalCommandID CommandID = "TransactionReversal"

	// PayTaxToKRACommandID is applied when remitting tax directly to Kenya Revenue Authority (KRA).
	PayTaxToKRACommandID CommandID = "PayTaxToKRA"

	// BusinessBuyGoodsCommandID is applied when transferring funds from a business account to a Buy Goods till (B2B).
	BusinessBuyGoodsCommandID CommandID = "BusinessBuyGoods"

	// DisburseFundsToBusinessCommandID is applied when transferring funds between business accounts.
	DisburseFundsToBusinessCommandID CommandID = "DisburseFundsToBusiness"
)

// IdentifierType is the type of organization receiving or initiating the transaction.
type IdentifierType uint8

const (
	// ShortcodeIdentifierType represents an organization shortcode (Paybill or Store/Till number).
	ShortcodeIdentifierType IdentifierType = 4
)

// TransactionType is used to identify the type of the transaction being made.
type TransactionType string

const (
	// CustomerBuyGoodsOnlineTransactionType is used for STK push requests for Buy Goods (Till numbers).
	CustomerBuyGoodsOnlineTransactionType = "CustomerBuyGoodsOnline"

	// CustomerPayBillOnlineTransactionType is used for STK push requests for Paybill numbers.
	CustomerPayBillOnlineTransactionType = "CustomerPayBillOnline"
)

// AuthorizationResponse contains the OAuth 2.0 access token returned by the Safaricom Daraja API.
type AuthorizationResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   string `json:"expires_in"`

	// Internal fields
	setAt time.Time
}

// STKPushRequest represents the payload for initiating an STK Push (Lipa Na M-Pesa Online).
type STKPushRequest struct {
	// BusinessShortCode is the organization shortcode (Paybill or Store Number - a 5 to 7-digit number)
	// used to identify an organization and process the transaction.
	BusinessShortCode uint `json:"BusinessShortCode,string"`

	// Password is a base64 encoded string used for encrypting the request, combining
	// BusinessShortCode + Passkey + Timestamp. Generated automatically if using client helper methods.
	Password string `json:"Password"`

	// Timestamp of the transaction in the format YYYYMMDDHHmmss.
	Timestamp string `json:"Timestamp"`

	// TransactionType identifies the transaction type.
	// Use CustomerPayBillOnlineTransactionType or CustomerBuyGoodsOnlineTransactionType.
	TransactionType TransactionType `json:"TransactionType"`

	// Amount to be transacted which will be deducted from the customer.
	Amount uint `json:"Amount,string"`

	// PartyA is the customer phone number sending money in format 2547XXXXXXXX or 2541XXXXXXXX.
	// Defined as uint64 to support 12-digit phone numbers safely across all system architectures.
	PartyA uint64 `json:"PartyA,string"`

	// PartyB is the organization receiving the funds (Paybill shortcode or Store/Till number).
	PartyB uint `json:"PartyB,string"`

	// PhoneNumber is the mobile number to receive the STK Pin Prompt, typically matching PartyA.
	PhoneNumber uint64 `json:"PhoneNumber,string"`

	// CallBackURL is a secure HTTPS endpoint where M-Pesa will deliver the asynchronous transaction result.
	CallBackURL string `json:"CallBackURL"`

	// AccountReference is a customer/order identifier displayed on the phone PIN prompt (max 12 characters).
	AccountReference string `json:"AccountReference"`

	// TransactionDesc is an additional description sent with the request (max 13 characters).
	TransactionDesc string `json:"TransactionDesc"`
}

// STKPaybillRequest contains parameters for initiating a Lipa Na M-Pesa Online STK Push for a Paybill shortcode.
type STKPaybillRequest struct {
	// PaybillNumber is your organization's Paybill shortcode (acts as BusinessShortCode and PartyB).
	PaybillNumber uint `json:"PaybillNumber"`

	// PhoneNumber is the customer phone number sending money (in format 2547XXXXXXXX or 2541XXXXXXXX).
	PhoneNumber uint64 `json:"PhoneNumber"`

	// Amount to be deducted from customer.
	Amount uint `json:"Amount"`

	// CallBackURL is the HTTPS webhook URL where M-Pesa delivers the transaction outcome.
	CallBackURL string `json:"CallBackURL"`

	// AccountReference is the account/order identifier displayed on the user's PIN prompt (e.g. invoice or account number).
	AccountReference string `json:"AccountReference"`

	// TransactionDesc is any brief description of the payment.
	TransactionDesc string `json:"TransactionDesc,omitempty"`

	// Passkey is optional; if omitted, the client's default Passkey is used.
	Passkey string `json:"Passkey,omitempty"`
}

// STKBuyGoodsRequest contains parameters for initiating a Lipa Na M-Pesa Online STK Push for a Buy Goods Till Number.
type STKBuyGoodsRequest struct {
	// StoreShortCode is your Head Office / Store shortcode linked to your Daraja app (acts as BusinessShortCode).
	StoreShortCode uint `json:"StoreShortCode"`

	// TillNumber is the Buy Goods Till Number (acts as PartyB).
	TillNumber uint `json:"TillNumber"`

	// PhoneNumber is the customer phone number sending money (in format 2547XXXXXXXX or 2541XXXXXXXX).
	PhoneNumber uint64 `json:"PhoneNumber"`

	// Amount to be deducted from customer.
	Amount uint `json:"Amount"`

	// CallBackURL is the HTTPS webhook URL where M-Pesa delivers the transaction outcome.
	CallBackURL string `json:"CallBackURL"`

	// AccountReference is an optional reference displayed on the user's PIN prompt (defaults to TillNumber if empty).
	AccountReference string `json:"AccountReference,omitempty"`

	// TransactionDesc is any brief description of the payment.
	TransactionDesc string `json:"TransactionDesc,omitempty"`

	// Passkey is optional; if omitted, the client's default Passkey is used.
	Passkey string `json:"Passkey,omitempty"`
}

// Response represents the initial synchronous response returned by Daraja API upon accepting a request.
type Response struct {
	// CheckoutRequestID is a globally unique identifier of the processed checkout transaction request.
	CheckoutRequestID string `json:"CheckoutRequestID,omitempty"`

	// ConversationID is a globally unique identifier for the transaction request returned by M-Pesa upon successful submission.
	ConversationID string `json:"ConversationID,omitempty"`

	// CustomerMessage is a message suitable for displaying to the customer as an acknowledgement.
	CustomerMessage string `json:"CustomerMessage,omitempty"`

	// ErrorCode is a predefined error code indicating the failure reason.
	ErrorCode string `json:"errorCode,omitempty"`

	// ErrorMessage is a short descriptive message of the failure reason.
	ErrorMessage string `json:"errorMessage,omitempty"`

	// MerchantRequestID is a globally unique identifier for the submitted payment request.
	MerchantRequestID string `json:"MerchantRequestID,omitempty"`

	// OriginatorConversationID is a globally unique identifier returned by the API proxy upon submission.
	OriginatorConversationID string `json:"OriginatorConversationID,omitempty"`

	// ResponseCode is a numeric status code indicating submission status (0 = Success).
	ResponseCode string `json:"ResponseCode,omitempty"`

	// ResponseDescription provides details on the submission status.
	ResponseDescription string `json:"ResponseDescription,omitempty"`

	// ResultCode indicates transaction processing outcome when querying transaction status.
	ResultCode string `json:"ResultCode,omitempty"`

	// ResultDesc gives the human-readable processing status of the transaction.
	ResultDesc string `json:"ResultDesc,omitempty"`

	// RequestID is a unique identifier assigned to the HTTP request.
	RequestID string `json:"requestId,omitempty"`
}

// STKCallbackItem represents an individual metadata key-value item within STKCallbackMetadata.
type STKCallbackItem struct {
	Name  string      `json:"Name"`
	Value interface{} `json:"Value,omitempty"`
}

// STKCallbackMetadata holds the collection of metadata items returned for a successful STK Push transaction.
type STKCallbackMetadata struct {
	Item []STKCallbackItem `json:"Item"`
}

// STKCallback holds the transaction outcome data inside an STK Push callback.
type STKCallback struct {
	// MerchantRequestID is the identifier returned in the initial STK Push response.
	MerchantRequestID string `json:"MerchantRequestID"`

	// CheckoutRequestID is the identifier returned in the initial STK Push response.
	CheckoutRequestID string `json:"CheckoutRequestID"`

	// ResultCode is 0 for successful payment, or a non-zero code if cancelled/failed (e.g. 1032 = Cancelled by user).
	ResultCode int `json:"ResultCode"`

	// ResultDesc provides a human-readable description of the transaction outcome.
	ResultDesc string `json:"ResultDesc"`

	// CallbackMetadata contains additional receipt information (Amount, MpesaReceiptNumber, PhoneNumber, TransactionDate).
	// Only populated when ResultCode == 0.
	CallbackMetadata STKCallbackMetadata `json:"CallbackMetadata"`
}

// STKPushCallbackBody wraps the STKCallback inside an STKPushCallback webhook payload.
type STKPushCallbackBody struct {
	STKCallback STKCallback `json:"stkCallback"`
}

// STKPushCallback is the root JSON payload sent to your CallBackURL by Safaricom after an STK Push completes.
type STKPushCallback struct {
	Body STKPushCallbackBody `json:"Body"`
}

// IsSuccessful returns true if the initial API response indicates accepted processing (ResponseCode == "0" or ResultCode == "0").
func (r *Response) IsSuccessful() bool {
	if r == nil {
		return false
	}
	return r.ResultCode == "0" || (r.ResultCode == "" && r.ResponseCode == "0")
}

// IsSuccessful returns true if the STK callback indicates a successful customer payment (ResultCode == 0).
func (c *STKCallback) IsSuccessful() bool {
	return c != nil && c.ResultCode == 0
}

// ReceiptNumber returns the M-Pesa receipt number (e.g. "NLJ7RT61SV") from the callback metadata if present.
func (c *STKCallback) ReceiptNumber() string {
	if c == nil {
		return ""
	}
	for _, item := range c.CallbackMetadata.Item {
		if strings.EqualFold(item.Name, "MpesaReceiptNumber") {
			if s, ok := item.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

// Amount extracts the transacted monetary amount from CallbackMetadata if present.
func (c *STKCallback) Amount() (float64, bool) {
	if c == nil {
		return 0, false
	}
	for _, item := range c.CallbackMetadata.Item {
		if strings.EqualFold(item.Name, "Amount") {
			switch v := item.Value.(type) {
			case float64:
				return v, true
			case int:
				return float64(v), true
			case string:
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					return f, true
				}
			}
		}
	}
	return 0, false
}

// PhoneNumber extracts the customer phone number from CallbackMetadata if present.
func (c *STKCallback) PhoneNumber() (uint64, bool) {
	if c == nil {
		return 0, false
	}
	for _, item := range c.CallbackMetadata.Item {
		if strings.EqualFold(item.Name, "PhoneNumber") {
			switch v := item.Value.(type) {
			case float64:
				return uint64(v), true
			case uint64:
				return v, true
			case int64:
				return uint64(v), true
			case string:
				if u, err := strconv.ParseUint(v, 10, 64); err == nil {
					return u, true
				}
			}
		}
	}
	return 0, false
}

// TransactionDate extracts the transaction timestamp string (e.g. "20231028120000") from CallbackMetadata if present.
func (c *STKCallback) TransactionDate() string {
	if c == nil {
		return ""
	}
	for _, item := range c.CallbackMetadata.Item {
		if strings.EqualFold(item.Name, "TransactionDate") {
			switch v := item.Value.(type) {
			case string:
				return v
			case float64:
				return fmt.Sprintf("%.0f", v)
			}
		}
	}
	return ""
}

// Common Safaricom Daraja STK Push Result Codes.
const (
	ResultCodeSuccess            = 0
	ResultCodeInsufficientFunds  = 1
	ResultCodeLessThanMinimum    = 2
	ResultCodeMoreThanMaximum    = 3
	ResultCodeExceededLimit      = 4
	ResultCodeUserCancelled      = 1032
	ResultCodeTransactionTimeout = 1037
)

// IsUserCancelled returns true if the customer cancelled the STK Push prompt (ResultCode 1032).
func (c *STKCallback) IsUserCancelled() bool {
	return c != nil && c.ResultCode == ResultCodeUserCancelled
}

// IsTimeout returns true if the STK Push expired/timed out waiting for user input (ResultCode 1037).
func (c *STKCallback) IsTimeout() bool {
	return c != nil && c.ResultCode == ResultCodeTransactionTimeout
}

// ParseSTKCallback parses an incoming Safaricom Daraja STK Push callback JSON payload from an io.Reader.
func ParseSTKCallback(r io.Reader) (*STKCallback, error) {
	var payload STKPushCallback
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("mpesa: failed to decode STK callback JSON: %w", err)
	}
	return &payload.Body.STKCallback, nil
}

// ParseSTKCallbackBytes parses an incoming Safaricom Daraja STK Push callback JSON payload from raw bytes.
func ParseSTKCallbackBytes(data []byte) (*STKCallback, error) {
	var payload STKPushCallback
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("mpesa: failed to unmarshal STK callback JSON: %w", err)
	}
	return &payload.Body.STKCallback, nil
}

// UnmarshalSTKPushCallback decodes an STKPushCallback root payload from an io.Reader.
func UnmarshalSTKPushCallback(r io.Reader) (*STKPushCallback, error) {
	var payload STKPushCallback
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("mpesa: failed to decode STK callback JSON: %w", err)
	}
	return &payload, nil
}

// B2CRequest represents the payload for initiating a Business-to-Consumer payment.
type B2CRequest struct {
	// InitiatorName is the username of the M-Pesa B2C account API operator.
	InitiatorName string `json:"InitiatorName"`

	// SecurityCredential is the encrypted password of the API initiator.
	SecurityCredential string `json:"SecurityCredential"`

	// CommandID specifies the B2C transaction type:
	// - SalaryPaymentCommandID
	// - BusinessPaymentCommandID
	// - PromotionPaymentCommandID
	CommandID CommandID `json:"CommandID"`

	// Amount to be sent to the customer.
	Amount uint `json:"Amount"`

	// PartyA is the B2C organization shortcode from which funds will be deducted.
	PartyA uint `json:"PartyA"`

	// PartyB is the customer mobile number (in format 2547XXXXXXXX or 2541XXXXXXXX).
	PartyB uint64 `json:"PartyB"`

	// Remarks represents any additional comments associated with the transaction.
	Remarks string `json:"Remarks"`

	// QueueTimeOutURL is the HTTPS endpoint notified if the request times out in queue.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// ResultURL is the HTTPS endpoint notified upon processing completion.
	ResultURL string `json:"ResultURL"`

	// Occasion is optional additional information associated with the transaction.
	Occasion string `json:"Occasion"`
}

// ResultParameter holds an individual transaction detail parameter returned in M-Pesa callback payloads.
// Common keys returned by Daraja include:
//   - "TransactionAmount": The transacted monetary amount.
//   - "TransactionReceipt": The unique M-Pesa receipt number.
//   - "TransactionDate": The timestamp of the transaction (YYYYMMDDHHmmss).
//   - "ReceiverPartyPublicName": Recipient details.
//   - "B2CWorkingAccountAvailableFunds", "B2CUtilityAccountAvailableFunds": Account balance information.
type ResultParameter struct {
	Key   string      `json:"Key"`
	Value interface{} `json:"Value"`
}

// ResultParameters wraps a collection of ResultParameter items in an asynchronous callback payload.
type ResultParameters struct {
	ResultParameter []ResultParameter `json:"ResultParameter"`
}

// ReferenceItem holds a key-value reference pair in callback ReferenceData.
type ReferenceItem struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// ReferenceData holds reference items returned by M-Pesa callback results.
type ReferenceData struct {
	ReferenceItem ReferenceItem `json:"ReferenceItem"`
}

// CallbackResult represents the inner result payload of an asynchronous callback for B2C, Account Balance, Reversal, or Transaction Status queries.
type CallbackResult struct {
	// ConversationID is a globally unique identifier returned by M-Pesa for the transaction.
	ConversationID string `json:"ConversationID"`

	// OriginatorConversationID is a globally unique identifier returned by the API proxy.
	OriginatorConversationID string `json:"OriginatorConversationID"`

	// ReferenceData holds reference items returned with the callback.
	ReferenceData ReferenceData `json:"ReferenceData"`

	// ResultCode indicates processing outcome (0 = Success, non-zero = error).
	ResultCode int `json:"ResultCode"`

	// ResultDesc describes the status of the request processing.
	ResultDesc string `json:"ResultDesc"`

	// ResultParameters holds additional details such as amounts, receipts, or balances.
	ResultParameters ResultParameters `json:"ResultParameters"`

	// ResultType indicates whether the transaction was already sent to your listener.
	ResultType int `json:"ResultType"`

	// TransactionID is the unique M-Pesa transaction ID for the payment request.
	TransactionID string `json:"TransactionID"`
}

// Callback represents an asynchronous notification payload received from M-Pesa for B2C, Account Balance, Reversals, or Transaction Status queries.
type Callback struct {
	Result CallbackResult `json:"Result"`
}

// STKQueryRequest represents the payload for querying the status of an ongoing or completed STK Push transaction.
type STKQueryRequest struct {
	// BusinessShortCode is the organization shortcode (Paybill or Store number).
	BusinessShortCode uint `json:"BusinessShortCode,string"`

	// CheckoutRequestID is the identifier returned in the initial STK Push response.
	CheckoutRequestID string `json:"CheckoutRequestID"`

	// Password is a base64 encoded string combining BusinessShortCode + Passkey + Timestamp.
	Password string `json:"Password"`

	// Timestamp of the query request in format YYYYMMDDHHmmss.
	Timestamp string `json:"Timestamp"`
}

// RegisterC2BURLRequest represents the payload to register C2B validation and confirmation URLs with M-Pesa.
type RegisterC2BURLRequest struct {
	// ShortCode is the Paybill or Till number to register URLs for.
	ShortCode uint `json:"ShortCode"`

	// ResponseType specifies fallback behavior if validation URL is unreachable (Completed or Canceled).
	ResponseType ResponseType `json:"ResponseType"`

	// ConfirmationURL receives payment notifications upon completion. Must be HTTPS.
	ConfirmationURL string `json:"ConfirmationURL"`

	// ValidationURL receives validation requests prior to payment completion (if external validation is enabled). Must be HTTPS.
	ValidationURL string `json:"ValidationURL"`
}

// DynamicQRRequest represents the payload to generate a dynamic M-Pesa QR code image.
type DynamicQRRequest struct {
	// Amount for the transaction.
	Amount uint `json:"Amount"`

	// CreditPartyIdentifier is the recipient identifier (Mobile number, Till, Paybill, or Shortcode).
	CreditPartyIdentifier string `json:"CPI"`

	// MerchantName is the merchant name displayed to the customer.
	MerchantName string `json:"MerchantName"`

	// ReferenceNo is the transaction reference number.
	ReferenceNo string `json:"RefNo"`

	// Size of the square QR code image in pixels (e.g. "300").
	Size string `json:"Size"`

	// TransactionType represents the QR transaction type:
	// - PayMerchantBuyGoods ("BG")
	// - PaybillOrBusinessNumber ("PB")
	// - SendMoneyViaMobileNumber ("SM")
	// - SentToBusiness ("SB")
	// - WithdrawCashAtAgentTill ("WA")
	TransactionType DynamicQRTransactionType `json:"TrxCode"`
}

// DynamicQRResponse represents the response containing the generated QR code.
type DynamicQRResponse struct {
	// ImagePath is the absolute path to the decoded PNG image saved on disk.
	ImagePath string `json:"qr_path,omitempty"`

	// ErrorCode is a predefined error code if generation failed.
	ErrorCode string `json:"errorCode,omitempty"`

	// ErrorMessage describes the error reason.
	ErrorMessage string `json:"errorMessage,omitempty"`

	// QRCode is the base64 encoded QR image data string.
	QRCode string `json:"QRCode,omitempty"`

	// RequestID is the unique ID for the request.
	RequestID string `json:"requestId,omitempty"`

	// ResponseCode is a numeric status code indicating submission outcome.
	ResponseCode string `json:"ResponseCode,omitempty"`

	// ResponseDescription provides details on the submission outcome.
	ResponseDescription string `json:"ResponseDescription,omitempty"`
}

// TransactionStatusRequest represents the payload to query the status of an existing M-Pesa transaction.
type TransactionStatusRequest struct {
	// CommandID for the request: TransactionStatusQueryCommandID.
	CommandID CommandID `json:"CommandID"`

	// IdentifierType is the organization identifier type (typically ShortcodeIdentifierType = 4).
	IdentifierType IdentifierType `json:"IdentifierType"`

	// Initiator is the username of the API operator.
	Initiator string `json:"Initiator"`

	// Occasion is an optional parameter (up to 100 characters).
	Occasion string `json:"Occasion,omitempty"`

	// OriginatorConversationID is an optional query parameter if TransactionID is unavailable.
	OriginatorConversationID string `json:"OriginatorConversationID,omitempty"`

	// PartyA is the querying organization shortcode.
	PartyA uint `json:"PartyA"`

	// QueueTimeOutURL is the HTTPS endpoint for timeout notifications.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// Remarks are comments sent with the request (up to 100 characters).
	Remarks string `json:"Remarks"`

	// ResultURL is the HTTPS endpoint for transaction query results.
	ResultURL string `json:"ResultURL"`

	// SecurityCredential is the encrypted initiator password.
	SecurityCredential string `json:"SecurityCredential"`

	// TransactionID is the unique M-Pesa receipt ID to query (e.g., LKXXXXX).
	TransactionID string `json:"TransactionID"`
}

// AccountBalanceRequest represents the payload to query the balance of an M-Pesa shortcode.
type AccountBalanceRequest struct {
	// CommandID for the request: AccountBalanceCommandID.
	CommandID CommandID `json:"CommandID"`

	// IdentifierType is the organization identifier type (ShortcodeIdentifierType = 4).
	IdentifierType IdentifierType `json:"IdentifierType"`

	// Initiator is the username of the API operator.
	Initiator string `json:"Initiator"`

	// PartyA is the shortcode of the organization querying for the balance.
	// Defined as uint for consistency with shortcode fields across the SDK.
	PartyA uint `json:"PartyA"`

	// QueueTimeOutURL is the HTTPS endpoint for timeout notifications.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// Remarks are comments sent with the request (up to 100 characters).
	Remarks string `json:"Remarks"`

	// ResultURL is the HTTPS endpoint for balance results.
	ResultURL string `json:"ResultURL"`

	// SecurityCredential is the encrypted initiator password.
	SecurityCredential string `json:"SecurityCredential"`
}

// BusinessPayBillRequest represents the payload to transfer funds from a business account to a Paybill (B2B).
type BusinessPayBillRequest struct {
	// AccountReference is the destination account number (up to 13 characters).
	AccountReference string `json:"AccountReference"`

	// Amount is the transaction amount.
	Amount uint `json:"Amount"`

	// CommandID for the request: BusinessPayBillCommandID.
	CommandID CommandID `json:"CommandID"`

	// Initiator is the username of the API operator.
	Initiator string `json:"Initiator"`

	// Occasion is an optional parameter (up to 100 characters).
	Occasion string `json:"Occasion,omitempty"`

	// PartyA is your business shortcode from which funds will be deducted.
	PartyA uint `json:"PartyA"`

	// PartyB is the destination Paybill shortcode.
	PartyB uint `json:"PartyB"`

	// QueueTimeOutURL is the HTTPS endpoint for timeout notifications.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// ReceiverIdentifierType is the shortcode type to which funds are credited.
	// For this API, ShortcodeIdentifierType (4) is used.
	ReceiverIdentifierType IdentifierType `json:"RecieverIdentifierType"`

	// Remarks are comments sent with the transaction (up to 100 characters).
	Remarks string `json:"Remarks"`

	// Requester is the optional consumer mobile number on behalf of whom you are paying.
	Requester int64 `json:"Requester,omitempty"`

	// ResultURL is the HTTPS endpoint for transaction results.
	ResultURL string `json:"ResultURL"`

	// SecurityCredential is the encrypted initiator password.
	SecurityCredential string `json:"SecurityCredential"`

	// SenderIdentifierType is the shortcode type from which funds are deducted.
	SenderIdentifierType IdentifierType `json:"SenderIdentifierType"`
}

// ReversalRequest represents the payload to reverse an existing M-Pesa transaction.
type ReversalRequest struct {
	// Initiator is the username of the API operator.
	Initiator string `json:"Initiator"`

	// SecurityCredential is the encrypted initiator password.
	SecurityCredential string `json:"SecurityCredential"`

	// CommandID for the request: TransactionReversalCommandID.
	CommandID CommandID `json:"CommandID"`

	// TransactionID is the unique identifier of the transaction to reverse (e.g. LKXXXXX).
	TransactionID string `json:"TransactionID"`

	// Amount to be reversed.
	Amount uint `json:"Amount"`

	// ReceiverParty is the organization shortcode reversing the transaction.
	ReceiverParty uint `json:"ReceiverParty"`

	// ReceiverIdentifierType is the identifier type (typically ShortcodeIdentifierType = 4).
	ReceiverIdentifierType IdentifierType `json:"RecieverIdentifierType"`

	// ResultURL is the HTTPS endpoint for reversal results.
	ResultURL string `json:"ResultURL"`

	// QueueTimeOutURL is the HTTPS endpoint for timeout notifications.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// Remarks are comments sent with the reversal (up to 100 characters).
	Remarks string `json:"Remarks"`

	// Occasion is an optional parameter (up to 100 characters).
	Occasion string `json:"Occasion,omitempty"`
}

// TaxRemittanceRequest represents the payload to remit taxes directly to Kenya Revenue Authority (KRA).
type TaxRemittanceRequest struct {
	// Initiator is the username of the API operator.
	Initiator string `json:"Initiator"`

	// SecurityCredential is the encrypted initiator password.
	SecurityCredential string `json:"SecurityCredential"`

	// CommandID for the request: PayTaxToKRACommandID.
	CommandID CommandID `json:"CommandID"`

	// SenderIdentifierType is the shortcode type from which tax funds are deducted.
	SenderIdentifierType IdentifierType `json:"SenderIdentifierType"`

	// ReceiverIdentifierType is the shortcode type to which tax is credited (ShortcodeIdentifierType = 4).
	ReceiverIdentifierType IdentifierType `json:"RecieverIdentifierType"`

	// Amount to be remitted.
	Amount uint `json:"Amount"`

	// PartyA is your business shortcode.
	PartyA uint `json:"PartyA"`

	// PartyB is the KRA Paybill/shortcode (e.g. 572572).
	PartyB uint `json:"PartyB"`

	// AccountReference is the KRA Payment Registration Number (PRN).
	AccountReference string `json:"AccountReference"`

	// Remarks are comments sent with the remittance.
	Remarks string `json:"Remarks"`

	// QueueTimeOutURL is the HTTPS endpoint for timeout notifications.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// ResultURL is the HTTPS endpoint for transaction results.
	ResultURL string `json:"ResultURL"`
}

// BusinessBuyGoodsRequest represents the payload to transfer funds from a business account to a Buy Goods till (B2B).
type BusinessBuyGoodsRequest struct {
	// Initiator is the username of the API operator.
	Initiator string `json:"Initiator"`

	// SecurityCredential is the encrypted initiator password.
	SecurityCredential string `json:"SecurityCredential"`

	// CommandID for the request: BusinessBuyGoodsCommandID.
	CommandID CommandID `json:"CommandID"`

	// SenderIdentifierType is the shortcode type from which money is deducted.
	SenderIdentifierType IdentifierType `json:"SenderIdentifierType"`

	// ReceiverIdentifierType is the shortcode type to which money is credited (ShortcodeIdentifierType = 4).
	ReceiverIdentifierType IdentifierType `json:"RecieverIdentifierType"`

	// Amount to be transacted.
	Amount uint `json:"Amount"`

	// PartyA is your business shortcode.
	PartyA uint `json:"PartyA"`

	// PartyB is the recipient Merchant Buy Goods shortcode/till.
	PartyB uint `json:"PartyB"`

	// AccountReference is an optional account or invoice reference.
	AccountReference string `json:"AccountReference,omitempty"`

	// Remarks are comments sent with the transaction.
	Remarks string `json:"Remarks"`

	// QueueTimeOutURL is the HTTPS endpoint for timeout notifications.
	QueueTimeOutURL string `json:"QueueTimeOutURL"`

	// ResultURL is the HTTPS endpoint for transaction results.
	ResultURL string `json:"ResultURL"`

	// Requester is the optional consumer mobile number on behalf of whom you are paying.
	Requester int64 `json:"Requester,omitempty"`
}
