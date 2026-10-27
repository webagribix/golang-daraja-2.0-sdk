# Golang Daraja 2.0 SDK

[![Go Reference](https://pkg.go.dev/badge/github.com/webagribix/golang-daraja-2.0-sdk.svg)](https://pkg.go.dev/github.com/webagribix/golang-daraja-2.0-sdk)
[![Go Version](https://img.shields.io/github/go-mod/go-version/webagribix/golang-daraja-2.0-sdk)](https://golang.org)
[![Test](https://github.com/webagribix/golang-daraja-2.0-sdk/actions/workflows/test.yml/badge.svg)](https://github.com/webagribix/golang-daraja-2.0-sdk/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A modern, fast, and idiomatic Go client library for integrating Safaricom's **M-Pesa Daraja 2.0 APIs** into your Go backend microservices and web applications.

---

## Features

- **OAuth 2.0 Token Caching**: Automatic authentication and token lifecycle management with in-memory caching.
- **Lipa Na M-Pesa Online (STK Push)**: Prompt customers to enter their M-Pesa PIN and authorize payments directly from their mobile phones.
- **M-Pesa Express Query**: Check and poll the status of an ongoing STK Push request.
- **B2C (Business to Customer)**: Disburse funds directly from your business short code to customer mobile numbers (e.g. salaries, payouts, promotions).
- **B2B (Business to Business)**: Make payments directly to other Paybill numbers or Buy Goods merchant tills.
- **Transaction Reversal**: Initiate reversals for mistaken or disputed transactions.
- **Tax Remittance**: Pay taxes directly from your business account to the Kenya Revenue Authority (KRA).
- **Dynamic QR Codes**: Generate dynamic M-Pesa QR codes for merchant tills.
- **Account Balance & Status**: Enquire organization account balances and inspect status of previous transactions.
- **C2B URL Registration**: Register Validation and Confirmation callback endpoints for Paybill/Till transactions.
- **Custom Certificate Loading**: Pass custom or updated Safaricom public security certificates dynamically (`.cer` file or PEM byte slice) without hardcoding dependencies.
- **Structured Error Handling**: Inspect typed errors (`APIError`) exposing `StatusCode`, `ErrorCode`, `RequestID`, and `ErrorMessage`.

---

## Installation

```bash
go get github.com/webagribix/golang-daraja-2.0-sdk
```

---

## Quickstart

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	mpesa "github.com/webagribix/golang-daraja-2.0-sdk"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Initialize the M-Pesa client (EnvironmentSandbox or EnvironmentProduction)
	client := mpesa.NewApp(
		http.DefaultClient,
		"YOUR_CONSUMER_KEY",
		"YOUR_CONSUMER_SECRET",
		mpesa.EnvironmentSandbox,
	)

	// 1. Initiate Lipa Na M-Pesa Online (STK Push)
	stkResp, err := client.STKPush(ctx, "YOUR_PASSKEY", mpesa.STKPushRequest{
		BusinessShortCode: 174379,
		TransactionType:   mpesa.CustomerPayBillOnlineTransactionType,
		Amount:            10,
		PartyA:            254708374149,
		PartyB:            174379,
		PhoneNumber:       254708374149,
		CallBackURL:       "https://yourdomain.com/callbacks/stk",
		AccountReference:  "INV-10023",
		TransactionDesc:   "Payment for Order #10023",
	})
	if err != nil {
		log.Fatalf("STK Push failed: %v", err)
	}

	fmt.Printf("CheckoutRequestID: %s\n", stkResp.CheckoutRequestID)

	// 2. Query STK Push status
	queryResp, err := client.STKQuery(ctx, "YOUR_PASSKEY", mpesa.STKQueryRequest{
		BusinessShortCode: 174379,
		CheckoutRequestID: stkResp.CheckoutRequestID,
	})
	if err != nil {
		log.Fatalf("STK Query failed: %v", err)
	}

	fmt.Printf("Status: %s - %s\n", queryResp.ResponseCode, queryResp.ResponseDescription)
}
```

---

## Usage Examples

### 1. Handling Callback Notifications

#### STK Push Callback

```go
http.HandleFunc("/callbacks/stk", func(w http.ResponseWriter, r *http.Request) {
	callback, err := mpesa.UnmarshalSTKPushCallback(r.Body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	cb := callback.Body.STKCallback
	if cb.ResultCode == 0 {
		log.Printf("Payment Success! CheckoutID: %s", cb.CheckoutRequestID)
		for _, item := range cb.CallbackMetadata.Item {
			log.Printf("Metadata: %s = %v", item.Name, item.Value)
		}
	} else {
		log.Printf("Payment Failed (%d): %s", cb.ResultCode, cb.ResultDesc)
	}

	w.WriteHeader(http.StatusOK)
})
```

#### General Callbacks (B2C, Balance, Reversal)

```go
http.HandleFunc("/callbacks/b2c", func(w http.ResponseWriter, r *http.Request) {
	callback, err := mpesa.UnmarshalCallback(r.Body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	result := callback.Result
	log.Printf("ConversationID: %s, ResultCode: %d, ResultDesc: %s",
		result.ConversationID, result.ResultCode, result.ResultDesc)

	w.WriteHeader(http.StatusOK)
})
```

---

### 2. Business to Customer (B2C)

Disburse funds to a customer's registered phone number:

```go
resp, err := client.B2C(ctx, "YOUR_INITIATOR_PASSWORD", mpesa.B2CRequest{
	InitiatorName:      "api_operator",
	CommandID:          mpesa.BusinessPaymentCommandID, // or SalaryPaymentCommandID, PromotionPaymentCommandID
	Amount:             500,
	PartyA:             600000,
	PartyB:             254708374149,
	Remarks:            "Refund for order #982",
	QueueTimeOutURL:    "https://yourdomain.com/callbacks/timeout",
	ResultURL:          "https://yourdomain.com/callbacks/b2c",
	Occasion:           "Refund",
})
```

---

### 3. Transaction Reversal

Reverse a mistaken or disputed transaction:

```go
resp, err := client.Reversal(ctx, "YOUR_INITIATOR_PASSWORD", mpesa.ReversalRequest{
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

### 4. Tax Remittance (KRA)

Pay business taxes directly to the Kenya Revenue Authority:

```go
resp, err := client.TaxRemittance(ctx, "YOUR_INITIATOR_PASSWORD", mpesa.TaxRemittanceRequest{
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

### 5. Custom Certificate Configuration

Safaricom rotates Daraja security certificates periodically. You can dynamically pass an updated certificate file or PEM string without waiting for a new SDK release:

```go
// Option A: From a certificate file (.cer / .pem)
err := client.WithCertificateFile("./certs/safaricom_prod_2026.cer")
if err != nil {
	log.Fatalf("Failed to load certificate: %v", err)
}

// Option B: From raw PEM bytes (e.g. loaded from environment variable or secret manager)
client.WithCertificatePEM([]byte("-----BEGIN CERTIFICATE-----\n..."))
```

---

### 6. Structured Error Handling

Inspect typed errors returned from the Safaricom gateway:

```go
stkResp, err := client.STKPush(ctx, "passkey", req)
if err != nil {
	var apiErr *mpesa.APIError
	if errors.As(err, &apiErr) {
		log.Printf("HTTP Status: %d", apiErr.StatusCode)
		log.Printf("Request ID: %s", apiErr.RequestID)
		log.Printf("Error Code: %s", apiErr.ErrorCode)
		log.Printf("Error Message: %s", apiErr.ErrorMessage)
	} else {
		log.Printf("Generic error: %v", err)
	}
}
```

---

## Supported APIs Summary

| API | Method | Description |
|---|---|---|
| **STK Push** | `client.STKPush` | Prompts user on phone to enter PIN |
| **STK Query** | `client.STKQuery` | Checks status of an STK push transaction |
| **B2C** | `client.B2C` | Disburses money to customer mobile number |
| **Business PayBill** | `client.BusinessPayBill` | Pays money from short code to PayBill number |
| **Business Buy Goods** | `client.BusinessBuyGoods` | Pays money from short code to Merchant Till |
| **Reversal** | `client.Reversal` | Reverses an M-Pesa transaction |
| **Tax Remittance** | `client.TaxRemittance` | Remits taxes to KRA short code |
| **Account Balance** | `client.GetAccountBalance` | Enquires balance on till/paybill account |
| **Transaction Status** | `client.GetTransactionStatus` | Queries status of any transaction |
| **Dynamic QR** | `client.DynamicQR` | Generates dynamic M-Pesa QR code |
| **C2B Register URL** | `client.RegisterC2BURL` | Registers confirmation and validation URLs |

---

## Testing

Run tests and race detector checks:

```bash
go test -v -race ./...
```

---

## License

This project is licensed under the [MIT License](LICENCE). Original work Copyright (c) 2021 Joram Wambugu. Revisions and Daraja 2.0 modernization Copyright (c) 2026 Raphael Webala (Nick Webala).
