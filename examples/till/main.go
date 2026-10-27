package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	mpesa "github.com/webagribix/golang-daraja-2.0-sdk"
)

// loadDotEnv parses a simple key=value .env file without external dependencies
func loadDotEnv(filepath string) (map[string]string, error) {
	envMap := make(map[string]string)
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			envMap[key] = val
		}
	}
	return envMap, scanner.Err()
}

func getVal(flagVal string, envMap map[string]string, keys ...string) string {
	if strings.TrimSpace(flagVal) != "" {
		return strings.TrimSpace(flagVal)
	}
	for _, k := range keys {
		if envMap != nil {
			if v, ok := envMap[k]; ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		if v := os.Getenv(k); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func main() {
	envFileFlag := flag.String("env", ".env", "Path to .env configuration file")
	phoneFlag := flag.String("phone", "", "Phone number to send STK prompt (e.g., 0712345678, +254712345678, or 254712345678)")
	amountFlag := flag.Uint("amount", 1, "Amount in KES to charge")
	flag.Parse()

	fmt.Println("==================================================")
	fmt.Println("  M-Pesa Daraja 2.0 SDK - Till (Buy Goods) Tester ")
	fmt.Println("==================================================")

	var envMap map[string]string
	if *envFileFlag != "" {
		loaded, err := loadDotEnv(*envFileFlag)
		if err != nil {
			if !os.IsNotExist(err) || *envFileFlag != ".env" {
				log.Printf("[Warning] Could not load env file %s: %v\n", *envFileFlag, err)
			}
		} else {
			envMap = loaded
			fmt.Printf(">> Successfully loaded environment from: %s\n", *envFileFlag)
		}
	}

	rawEnv := getVal("", envMap, "DARAJA_ENV", "MPESA_ENV")
	env := mpesa.EnvironmentSandbox
	if strings.ToLower(rawEnv) == "production" || strings.ToLower(rawEnv) == "prod" {
		env = mpesa.EnvironmentProduction
		fmt.Println(">> Target Environment: PRODUCTION (Live M-Pesa)")
	} else {
		fmt.Println(">> Target Environment: SANDBOX (Test Mode)")
	}

	consumerKey := getVal("", envMap, "DARAJA_CONSUMER_KEY", "MPESA_CONSUMER_KEY")
	consumerSecret := getVal("", envMap, "DARAJA_CONSUMER_SECRET", "MPESA_CONSUMER_SECRET")
	passkey := getVal("", envMap, "DARAJA_PASSKEY", "MPESA_PASSKEY")
	shortcodeStr := getVal("", envMap, "DARAJA_SHORTCODE", "MPESA_STORE_NUMBER")
	tillStr := getVal("", envMap, "DARAJA_TILL_NUMBER", "MPESA_TILL_NUMBER")
	callbackURL := getVal("", envMap, "DARAJA_CALLBACK_URL", "MPESA_CALLBACK_URL")

	if consumerKey == "" || consumerSecret == "" {
		log.Fatalf("Error: Consumer Key and Consumer Secret are required (set in %s or environment)", *envFileFlag)
	}
	if passkey == "" {
		log.Fatalf("Error: Passkey is required for STK Push (set in %s or environment)", *envFileFlag)
	}
	if shortcodeStr == "" {
		log.Fatalf("Error: Shortcode (Store / Business Shortcode) is required (set in %s or environment)", *envFileFlag)
	}

	if tillStr == "" {
		tillStr = shortcodeStr
	}

	storeShortcode, err := strconv.ParseUint(shortcodeStr, 10, 64)
	if err != nil {
		log.Fatalf("Error: Invalid shortcode %q: %v", shortcodeStr, err)
	}

	tillNumber, err := strconv.ParseUint(tillStr, 10, 64)
	if err != nil {
		log.Fatalf("Error: Invalid till number %q: %v", tillStr, err)
	}

	phoneInput := *phoneFlag
	if phoneInput == "" {
		phoneInput = getVal("", envMap, "DARAJA_PHONE", "MPESA_PHONE")
	}

	if phoneInput == "" {
		fmt.Print("\nEnter Phone Number to prompt (e.g. 0712345678 or 254712345678): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		phoneInput = strings.TrimSpace(input)
	}

	if phoneInput == "" {
		log.Fatalf("Error: Phone number is required. Pass --phone <number>")
	}

	// Normalize phone number to standard 254XXXXXXXXX
	normalizedPhoneStr, err := mpesa.NormalizePhoneNumber(phoneInput)
	if err != nil {
		log.Fatalf("Phone normalization error: %v", err)
	}
	phoneUint64, _ := strconv.ParseUint(normalizedPhoneStr, 10, 64)

	fmt.Println("\n--- Configuration Summary ---")
	fmt.Printf("Store Shortcode (BusinessShortCode): %d\n", storeShortcode)
	fmt.Printf("Till Number (PartyB):                %d\n", tillNumber)
	fmt.Printf("Input Phone:                         %s\n", phoneInput)
	fmt.Printf("Normalized Phone (PartyA):           %s\n", normalizedPhoneStr)
	fmt.Printf("Amount:                              KES %d\n", *amountFlag)
	fmt.Printf("Callback URL:                        %s\n", callbackURL)
	fmt.Println("-----------------------------")

	// Initialize the M-Pesa client using AWS-style Config
	client, err := mpesa.New(mpesa.Config{
		ConsumerKey:    consumerKey,
		ConsumerSecret: consumerSecret,
		Passkey:        passkey,
		Environment:    env,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Daraja client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	fmt.Println("\n[1/3] Authenticating with Safaricom Daraja OAuth 2.0...")
	token, err := client.GenerateAccessToken(ctx)
	if err != nil {
		var apiErr *mpesa.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("OAuth Error: status=%d, code=%s, msg=%s", apiErr.StatusCode, apiErr.ErrorCode, apiErr.ErrorMessage)
		}
		log.Fatalf("OAuth Error: %v", err)
	}
	fmt.Printf(">> Access Token successfully generated (Length: %d chars)\n", len(token))

	fmt.Println("\n[2/3] Initiating Lipa Na M-Pesa Online (Buy Goods STK Push)...")
	stkReq := mpesa.STKPushRequest{
		BusinessShortCode: uint(storeShortcode),
		TransactionType:   mpesa.CustomerBuyGoodsOnlineTransactionType,
		Amount:            *amountFlag,
		PartyA:            phoneUint64,
		PartyB:            uint(tillNumber),
		PhoneNumber:       phoneUint64,
		CallBackURL:       callbackURL,
		AccountReference:  "TillTest",
		TransactionDesc:   "Testing Buy Goods Till",
	}

	// Passkey automatically inferred from client Config
	stkResp, err := client.STKPush(ctx, "", stkReq)
	if err != nil {
		var apiErr *mpesa.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("\n>> Daraja STK Push Error [HTTP %d]:\n   Error Code: %s\n   Request ID: %s\n   Message: %s\n",
				apiErr.StatusCode, apiErr.ErrorCode, apiErr.RequestID, apiErr.ErrorMessage)
		}
		log.Fatalf("STK Push failed: %v", err)
	}

	fmt.Println("\n>> SUCCESS: STK Push sent to phone!")
	fmt.Printf("   Response Code:        %s\n", stkResp.ResponseCode)
	fmt.Printf("   Response Description: %s\n", stkResp.ResponseDescription)
	fmt.Printf("   Merchant Request ID:  %s\n", stkResp.MerchantRequestID)
	fmt.Printf("   Checkout Request ID:  %s\n", stkResp.CheckoutRequestID)
	fmt.Printf("   Customer Message:     %s\n", stkResp.CustomerMessage)

	fmt.Println("\n>> Please check your phone now! Enter M-Pesa PIN or Cancel to see real-time status:")
	fmt.Println("[3/3] Polling transaction outcome in real-time (up to 60s)...")

	pollResp, err := client.PollSTKStatus(ctx, "", uint(storeShortcode), stkResp.CheckoutRequestID, 60*time.Second, 3*time.Second)
	if err != nil {
		fmt.Printf(">> Poll ended: %v\n", err)
	} else {
		fmt.Println("\n==================================================")
		fmt.Println("             REAL-TIME TRANSACTION RESULT         ")
		fmt.Println("==================================================")
		fmt.Printf("Result Code:        %s\n", pollResp.ResultCode)
		fmt.Printf("Result Description: %s\n", pollResp.ResultDesc)
		if pollResp.IsSuccessful() {
			fmt.Println(">> Status: SUCCESSFUL PAYMENT! (Money Received)")
		} else {
			fmt.Printf(">> Status: FAILED / CANCELLED (%s)\n", pollResp.ResultDesc)
		}
		fmt.Println("==================================================")
	}

	fmt.Println("\nWebhook notification will also be delivered to:", callbackURL)
}
