# Golang Daraja 2.0 SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/webagribix/golang-daraja-2.0-sdk.svg)](https://pkg.go.dev/github.com/webagribix/golang-daraja-2.0-sdk)
[![Go Version](https://img.shields.io/github/go-mod/go-version/webagribix/golang-daraja-2.0-sdk)](https://golang.org)
[![Test](https://github.com/webagribix/golang-daraja-2.0-sdk/actions/workflows/test.yml/badge.svg)](https://github.com/webagribix/golang-daraja-2.0-sdk/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A modern, fast, and idiomatic Go client library for integrating Safaricom's **M-Pesa Daraja 2.0 APIs** into your Go backend microservices and web applications.

Designed with an AWS SDK-style clean configuration pattern, automatic phone normalization, token caching, and real-time status polling.

---

## Features

- **AWS SDK-Style Client Config**: Simple `mpesa.New(mpesa.Config{...})` setup.
- **Phone Number Normalization**: Automatically converts any Kenyan phone format (`07...`, `01...`, `+254...`, `7...`) into standard `254XXXXXXXXX`.
- **Lipa Na M-Pesa Online (STK Push)**: Supports both **PayBill** and **Buy Goods (Till Number)** payments.
- **Real-Time Outcome Polling (`PollSTKStatus`)**: In-process polling helper that waits for the user to complete their PIN prompt and returns immediate results (`Success`, `Cancelled`, `Wrong PIN`, `Insufficient Funds`).
- **OAuth 2.0 Token Caching**: Automatic token generation and in-memory lifecycle caching (~55 min TTL).
- **Webhook & Callback Helpers**: Pre-built deserializers with `.IsSuccessful()` and `.ReceiptNumber()` helper methods.
- **B2C (Business to Customer)**: Disburse funds directly from your business short code to customer mobile numbers (salaries, payouts, refunds).
- **B2B (Business to Business)**: Make payments to other Paybill numbers or Buy Goods merchant tills.
- **Transaction Reversal**: Initiate reversals for mistaken or disputed transactions.
- **Tax Remittance**: Pay taxes directly from your business account to the Kenya Revenue Authority (KRA).
- **Account Balance & Status**: Enquire organization account balances and inspect status of previous transactions.
- **Dynamic QR Codes**: Generate dynamic M-Pesa QR codes for merchant tills.
- **C2B URL Registration**: Register Validation and Confirmation callback endpoints.
- **Dynamic Certificate Loading**: Pass custom or updated Safaricom public security certificates (`.cer` file or PEM byte slice) without code changes.
- **Structured Error Handling**: Typed errors (`APIError`) exposing `StatusCode`, `ErrorCode`, `RequestID`, and `ErrorMessage`.

---

## Installation

```bash
go get github.com/webagribix/golang-daraja-2.0-sdk
```

---

## Quickstart: How to Use in Your Application

Similar to how you configure AWS or Backblaze SDKs, initialize the client using `mpesa.Config`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	mpesa "github.com/webagribix/golang-daraja-2.0-sdk"
)

func main() {
	ctx := context.Background()

	// 1. Initialize Daraja Client
	client, err := mpesa.New(mpesa.Config{
		ConsumerKey:    "YOUR_CONSUMER_KEY",
		ConsumerSecret: "YOUR_CONSUMER_SECRET",
		Passkey:        "YOUR_ONLINE_PASSKEY", // Optional: sets default passkey for STK operations
		Environment:    mpesa.EnvironmentProduction, // or mpesa.EnvironmentSandbox
	})
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	// 2. Normalize customer phone number (accepts 07..., 01..., +254...)
	phone, err := mpesa.NormalizePhoneNumberUint64("0759345391")
	if err != nil {
		log.Fatalf("invalid phone: %v", err)
	}

	// 3. Send STK Push prompt (Buy Goods Till example)
	stkResp, err := client.STKPush(ctx, "", mpesa.STKPushRequest{
		BusinessShortCode: 7372538,                             // Store Number
		TransactionType:   mpesa.CustomerBuyGoodsOnlineTransactionType, // For Till Number
		Amount:            1,
		PartyA:            uint(phone),
		PartyB:            3325969,                             // Till Number
		PhoneNumber:       phone,
		CallBackURL:       "https://yourdomain.com/api/callbacks/mpesa",
		AccountReference:  "Order-1049",
		TransactionDesc:   "Payment for Order #1049",
	})
	if err != nil {
		log.Fatalf("STK Push error: %v", err)
	}

	fmt.Printf("STK Prompt sent! CheckoutID: %s\n", stkResp.CheckoutRequestID)

	// 4. Real-time Outcome Polling (Polls up to 45s while user enters PIN)
	result, err := client.PollSTKStatus(ctx, "", 7372538, stkResp.CheckoutRequestID, 45*time.Second, 3*time.Second)
	if err != nil {
		log.Fatalf("Polling error: %v", err)
	}

	if result.IsSuccessful() {
		fmt.Printf("Payment Successful! Description: %s\n", result.ResultDesc)
	} else {
		fmt.Printf("Payment Failed/Cancelled (%s): %s\n", result.ResultCode, result.ResultDesc)
	}
}
```

---

## Detailed Usage Guides

### 1. PayBill vs Buy Goods (Till Number)

| Parameter | PayBill Payment | Buy Goods (Till Number) |
|---|---|---|
| `TransactionType` | `mpesa.CustomerPayBillOnlineTransactionType` | `mpesa.CustomerBuyGoodsOnlineTransactionType` |
| `BusinessShortCode` | PayBill Number (e.g. `174379`) | Store Number / Head Office Shortcode |
| `PartyB` | PayBill Number | Customer-facing Till Number |
| `AccountReference` | Account / Invoice Number | Till Name or Reference |

---

### 2. Handling Webhook Callbacks

Safaricom sends the definitive payment result asynchronously to your `CallBackURL`.

```go
package main

import (
	"log"
	"net/http"

	mpesa "github.com/webagribix/golang-daraja-2.0-sdk"
)

func handleMpesaCallback(w http.ResponseWriter, r *http.Request) {
	callback, err := mpesa.UnmarshalSTKPushCallback(r.Body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	cb := callback.Body.STKCallback
	if cb.IsSuccessful() {
		receipt := cb.ReceiptNumber() // Extracts receipt (e.g. SI28X912)
		log.Printf("Payment SUCCESS! Receipt: %s, Checkout: %s", receipt, cb.CheckoutRequestID)
	} else {
		log.Printf("Payment FAILED/CANCELLED: Code=%d, Desc=%s", cb.ResultCode, cb.ResultDesc)
	}

	// Always respond 200 OK to acknowledge receipt
	w.WriteHeader(http.StatusOK)
}
```

---

### 3. Business to Customer (B2C Payouts & Refunds)

```go
client, _ := mpesa.New(mpesa.Config{
	ConsumerKey:       "KEY",
	ConsumerSecret:    "SECRET",
	InitiatorPassword: "INITIATOR_PASSWORD",
	Environment:       mpesa.EnvironmentProduction,
})

resp, err := client.B2C(ctx, "", mpesa.B2CRequest{
	InitiatorName:   "api_operator",
	CommandID:       mpesa.BusinessPaymentCommandID, // or SalaryPaymentCommandID, PromotionPaymentCommandID
	Amount:          500,
	PartyA:          600000,
	PartyB:          254759345391,
	Remarks:         "Refund for order #982",
	QueueTimeOutURL: "https://yourdomain.com/callbacks/timeout",
	ResultURL:       "https://yourdomain.com/callbacks/b2c",
})
```

---

### 4. Transaction Reversal

Reverse a mistaken or disputed transaction:

```go
resp, err := client.Reversal(ctx, "", mpesa.ReversalRequest{
	Initiator:              "api_operator",
	TransactionID:          "LK12345678",
	Amount:                 100,
	ReceiverParty:          600000,
	RecieverIdentifierType: mpesa.ShortcodeIdentifierType,
	QueueTimeOutURL:        "https://yourdomain.com/callbacks/timeout",
	ResultURL:              "https://yourdomain.com/callbacks/reversal",
	Remarks:                "Erroneous transaction reversal",
})
```

---

### 5. Tax Remittance (KRA)

Pay business taxes directly to the Kenya Revenue Authority:

```go
resp, err := client.TaxRemittance(ctx, "", mpesa.TaxRemittanceRequest{
	Initiator:        "api_operator",
	Amount:           15000,
	PartyA:           600000,
	PartyB:           572572, // KRA Paybill
	AccountReference: "PRN240192830192", // KRA Payment Registration Number
	Remarks:          "VAT remittance for Q1",
	QueueTimeOutURL:  "https://yourdomain.com/callbacks/timeout",
	ResultURL:        "https://yourdomain.com/callbacks/kra",
})
```

---

### 6. Dynamic Certificate Management

When Safaricom rotates its public certificate, pass the updated certificate directly in your config without waiting for a new SDK release:

```go
client, err := mpesa.New(mpesa.Config{
	ConsumerKey:     "KEY",
	ConsumerSecret:  "SECRET",
	CertificateFile: "./certs/ProductionCertificate.cer", // Load from file
	// OR: CertificatePEM: []byte(os.Getenv("SAFARICOM_CERT_PEM")), // From ENV
	Environment:     mpesa.EnvironmentProduction,
})
```

---

### 7. Structured Error Inspection

Safaricom returns structured JSON error responses with exact error codes. You can inspect them using Go's `errors.As`:

```go
stkResp, err := client.STKPush(ctx, "", req)
if err != nil {
	var apiErr *mpesa.APIError
	if errors.As(err, &apiErr) {
		log.Printf("HTTP Status: %d", apiErr.StatusCode)
		log.Printf("Error Code:  %s", apiErr.ErrorCode)
		log.Printf("Request ID:  %s", apiErr.RequestID)
		log.Printf("Message:     %s", apiErr.ErrorMessage)
	} else {
		log.Printf("Network/client error: %v", err)
	}
}
```

---

## Interactive Local Testing

An interactive testing runner for Till Numbers and PayBills is provided in `examples/till`:

```bash
go run ./examples/till/main.go --env .env --phone 0759345391 --amount 1
```

---

## License

This project is licensed under the [MIT License](LICENCE). Original work Copyright (c) 2021 Joram Wambugu. Revisions, Daraja 2.0 modernization, and maintenance Copyright (c) 2026 Nick Webala (Nick Webala).
