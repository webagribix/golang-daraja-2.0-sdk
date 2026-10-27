package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	mpesa "github.com/webagribix/golang-daraja-2.0-sdk"
)

// Helper to read from env or prompt user via CLI
func getEnvOrPrompt(reader *bufio.Reader, key, promptDefault, description string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	fmt.Printf("%s [%s]: ", description, promptDefault)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return promptDefault
	}
	return input
}

func parseUint(s string) uint {
	val, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		log.Fatalf("Invalid number '%s': %v", s, err)
	}
	return uint(val)
}

func parseUint64(s string) uint64 {
	val, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		log.Fatalf("Invalid number '%s': %v", s, err)
	}
	return val
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("==================================================")
	fmt.Println("  M-Pesa Daraja 2.0 SDK - Till (Buy Goods) Tester ")
	fmt.Println("==================================================")
	fmt.Println()

	envChoice := getEnvOrPrompt(reader, "MPESA_ENV", "sandbox", "Environment ('sandbox' or 'production')")
	var env mpesa.Environment = mpesa.EnvironmentSandbox
	if strings.ToLower(envChoice) == "production" || strings.ToLower(envChoice) == "prod" {
		env = mpesa.EnvironmentProduction
		fmt.Println(">> Running in PRODUCTION mode")
	} else {
		fmt.Println(">> Running in SANDBOX mode")
	}

	consumerKey := getEnvOrPrompt(reader, "MPESA_CONSUMER_KEY", "", "Daraja Consumer Key")
	consumerSecret := getEnvOrPrompt(reader, "MPESA_CONSUMER_SECRET", "", "Daraja Consumer Secret")
	passkey := getEnvOrPrompt(reader, "MPESA_PASSKEY", "", "Online Passkey")

	fmt.Println()
	fmt.Println("Note for Buy Goods (Till):")
	fmt.Println("- 'Store / Head Office ShortCode' is the shortcode assigned to your Daraja App.")
	fmt.Println("- 'Till Number' is your actual customer-facing Till Number (PartyB).")
	fmt.Println("- If unsure or in sandbox, both may be the same number.")
	fmt.Println()

	storeNumberStr := getEnvOrPrompt(reader, "MPESA_STORE_NUMBER", "", "Store Number / Business ShortCode")
	tillNumberStr := getEnvOrPrompt(reader, "MPESA_TILL_NUMBER", storeNumberStr, "Till Number (PartyB)")
	phoneStr := getEnvOrPrompt(reader, "MPESA_PHONE_NUMBER", "254700000000", "Phone Number to prompt (format: 2547XXXXXXXX)")
	amountStr := getEnvOrPrompt(reader, "MPESA_AMOUNT", "1", "Amount in KES")
	callbackURL := getEnvOrPrompt(reader, "MPESA_CALLBACK_URL", "https://webhook.site/test", "Callback URL (https)")

	storeShortCode := parseUint(storeNumberStr)
	tillNumber := parseUint(tillNumberStr)
	phoneNumber := parseUint64(phoneStr)
	amount := parseUint(amountStr)

	// Initialize the client
	client := mpesa.NewApp(http.DefaultClient, consumerKey, consumerSecret, env)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("\n1. Testing OAuth 2.0 Access Token Generation...")
	token, err := client.GenerateAccessToken(ctx)
	if err != nil {
		log.Fatalf("Authentication failed: %v", err)
	}
	fmt.Printf(">> Access Token successfully generated (Length: %d chars)\n", len(token))

	fmt.Println("\n2. Sending STK Push to Till...")
	stkReq := mpesa.STKPushRequest{
		BusinessShortCode: storeShortCode,
		TransactionType:   mpesa.CustomerBuyGoodsOnlineTransactionType, // "CustomerBuyGoodsOnline" for Till
		Amount:            amount,
		PartyA:            uint(phoneNumber),
		PartyB:            tillNumber,
		PhoneNumber:       phoneNumber,
		CallBackURL:       callbackURL,
		AccountReference:  "TillPaymentTest",
		TransactionDesc:   "Testing Buy Goods Till",
	}

	resp, err := client.STKPush(ctx, passkey, stkReq)
	if err != nil {
		var apiErr *mpesa.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf(">> Daraja API Error [HTTP %d]: Code=%s, RequestID=%s, Message=%s\n",
				apiErr.StatusCode, apiErr.ErrorCode, apiErr.RequestID, apiErr.ErrorMessage)
		}
		log.Fatalf(">> STK Push Error: %v\n", err)
	}

	fmt.Println(">> STK Push Sent Successfully!")
	fmt.Printf("   Response Code: %s\n", resp.ResponseCode)
	fmt.Printf("   Response Description: %s\n", resp.ResponseDescription)
	fmt.Printf("   Checkout Request ID: %s\n", resp.CheckoutRequestID)
	fmt.Printf("   Customer Message: %s\n", resp.CustomerMessage)

	fmt.Println("\n3. Waiting 10 seconds before querying transaction status...")
	time.Sleep(10 * time.Second)

	fmt.Println("4. Querying STK Push status...")
	queryResp, err := client.STKQuery(ctx, passkey, mpesa.STKQueryRequest{
		BusinessShortCode: storeShortCode,
		CheckoutRequestID: resp.CheckoutRequestID,
	})
	if err != nil {
		var apiErr *mpesa.APIError
		if errors.As(err, &apiErr) {
			log.Printf(">> Query API Error [HTTP %d]: Code=%s, Message=%s\n",
				apiErr.StatusCode, apiErr.ErrorCode, apiErr.ErrorMessage)
		} else {
			log.Printf(">> Query Error: %v\n", err)
		}
	} else {
		fmt.Printf(">> Query Result: Code=%s, Desc=%s\n",
			queryResp.ResponseCode, queryResp.ResponseDescription)
	}

	fmt.Println("\nTest Complete! Check your phone for the M-Pesa prompt, or check your callback URL for the webhook.")
}
