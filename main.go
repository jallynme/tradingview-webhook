package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

const (
	Version            = "0.0.1"
	BitkubAPI          = "https://api.bitkub.com/api/"
	LineNotify         = "https://notify-api.line.me/api/notify"
	HeaderBitkubAPIKey = "x-btk-apikey"
	ContentType        = "Content-Type"
	Authorization      = "Authorization"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.POST("/tradingview-webhook", func(c *gin.Context) {
		params := WebhookParams{}
		c.ShouldBind(&params)
		symbol := params.Symbol
		price := params.Price
		action := params.Action
		amountType := params.AmountType
		amount := 0.0

		if action != BuyActionType && action != SellActionType {
			c.AbortWithStatusJSON(400, gin.H{
				"message": "invalid webhook params",
			})
			return
		}
		walletBalance := Balances()
		switch amountType {
		case AllAvailable:
			if action == BuyActionType {
				if thb, ok := walletBalance["THB"]; ok {
					amount = thb.Available
				}
			} else if action == SellActionType {
				if symbol, ok := walletBalance[symbol]; ok {
					amount = symbol.Available
				}
			}
		case Percent:
			if action == BuyActionType {
				if thb, ok := walletBalance["THB"]; ok {
					temp := thb.Available * (amount / 100.0)
					amount = Round(temp, 2)
				}
			} else if action == SellActionType {
				if symbol, ok := walletBalance[symbol]; ok {
					temp := symbol.Available * (amount / 100.0)
					amount = Round(temp, 2)
				}
			}
		case LimitAmount:
			amount = params.Amount
		default:
			c.AbortWithStatusJSON(400, gin.H{
				"message": "invalid amount_type",
			})
			SendLineNotify("ivalid amount_type", "1", "1")
			return
		}
		message := fmt.Sprintf("Sending command %s %s price:%v amount: %v amount type: %v ", action, symbol, price, amount, amountType)
		SendLineNotify(message, "1", "1")

		switch action {
		case BuyActionType:
			price := params.Price
			Buy(symbol, price, amount, c)
		case SellActionType:
			price := params.Price
			Sell(symbol, price, amount, c)
		default:
			c.AbortWithStatusJSON(400, gin.H{
				"message": "invalid webhook params",
			})
		}
	})
	r.Run()
}

func Balances() WalletBalance {
	params := map[string]interface{}{}
	resp := call[WalletBalance]("market/balances", params)
	if resp.Error != nil {
		fmt.Printf("request failed with error code: %v\n description: %v\n", resp.Error.Code, resp.Error.Description)
	}

	if resp.Result != nil {
		b, _ := json.Marshal(resp.Result)
		fmt.Printf("resp: %v\n", string(b))
		return *resp.Result
	}
	return WalletBalance{}
}

func Wallet() {
	params := map[string]interface{}{}
	resp := call[map[string]float64]("market/wallet", params)
	if resp.Error != nil {
		fmt.Printf("request failed with error code: %v\n description: %v\n", resp.Error.Code, resp.Error.Description)
	}

	if resp.Result != nil {
		b, _ := json.Marshal(resp.Result)
		fmt.Printf("resp: %v\n", string(b))
	}
}

func Buy(symbol string, price float64, amount float64, c *gin.Context) {
	if !strings.HasPrefix("symbol", "THB_") {
		symbol = "THB_" + symbol
	}
	params := map[string]interface{}{
		"amt": amount,
		"sym": symbol,
		"rat": price,
		"typ": "limit",
	}
	resp := call[Order]("market/place-bid/test", params)
	if resp.Error != nil {
		desc := fmt.Sprintf("request failed with error: %v", resp.Error.Code) + resp.Error.Description
		SendLineNotify(desc, "1", "1")
		fmt.Printf("%v\n", desc)
		c.JSON(200, gin.H{"error": desc})
		return
	}

	if resp.Result != nil {
		b, _ := json.Marshal(resp.Result)
		respMessage := string(b)
		fmt.Printf("resp: %v\n", respMessage)
		SendLineNotify(respMessage, "1", "1")
		c.JSON(200, gin.H{"data": resp.Result})
		return
	}
	c.AbortWithStatusJSON(422, gin.H{"data": gin.H{"error": "no response from Bitkub"}})
}

func Sell(symbol string, price float64, amount float64, c *gin.Context) {
	if !strings.HasPrefix("symbol", "THB_") {
		symbol = "THB_" + symbol
	}
	params := map[string]interface{}{
		"amt": amount,
		"sym": symbol,
		"rat": price,
		"typ": "limit",
	}
	resp := call[Order]("market/place-ask/test", params)
	if resp.Error != nil {
		desc := fmt.Sprintf("request failed with error: %v ", resp.Error.Code) + resp.Error.Description
		SendLineNotify(desc, "1", "1")
		c.JSON(200, gin.H{"error": desc})
		return
	}

	if resp.Result != nil {
		b, _ := json.Marshal(resp.Result)
		respMessage := string(b)
		fmt.Printf("resp: %v\n", respMessage)
		SendLineNotify(respMessage, "1", "1")
		c.JSON(200, gin.H{"data": resp.Result})
		return
	}
	c.AbortWithStatusJSON(422, gin.H{"data": gin.H{"error": "no response from Bitkub"}})
}

func SendLineNotify(message string, stickerId string, stickerPackageId string) {

	data := url.Values{}
	data.Set("message", message)
	data.Set("stickerId", stickerId)
	data.Set("stickerPackageId", stickerPackageId)

	client := http.Client{}
	fmt.Printf("%+v  \n", data)
	req, _ := http.NewRequest("POST", LineNotify, strings.NewReader(data.Encode()))
	req.Header.Set(ContentType, "application/x-www-form-urlencoded")
	token := "Bearer " + os.Getenv("LINE_NOTIFY_TOKEN")
	req.Header.Set(Authorization, token)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("http request failed: %v \n", err)
	}
	respJSON := map[string]interface{}{}
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("ioutil.ReadAll error: %v\n", err)
	}
	if err := json.Unmarshal(respBody, &respJSON); err != nil {
		fmt.Printf("failed to parse JSON response: %v\n", err)
	}
	s := fmt.Sprintf("%v \n", respJSON)
	fmt.Println(s)
}

func call[T interface{}](path string, params map[string]interface{}) ResponseJSON[T] {
	apiKey := os.Getenv("BITKUB_API_KEY")
	secret := os.Getenv("BITKUB_API_SECRET")
	ts := time.Now().Unix()
	params["ts"] = ts
	sig := hmac.New(sha256.New, []byte(secret))
	data, _ := json.Marshal(params)
	sig.Write([]byte(string(data)))

	params["sig"] = hex.EncodeToString(sig.Sum(nil))
	client := http.Client{}
	url := BitkubAPI + path
	reqBodyBytes, _ := json.Marshal(params)
	reqBody := bytes.NewBuffer(reqBodyBytes)
	fmt.Printf("%+v  \n", reqBody)
	req, _ := http.NewRequest("POST", url, reqBody)
	req.Header.Set(ContentType, "application/json")
	req.Header.Set(HeaderBitkubAPIKey, apiKey)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("http request failed: %v \n", err)
	}

	respJSON := map[string]interface{}{}
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("ioutil.ReadAll error: %v\n", err)
	}
	if err := json.Unmarshal(respBody, &respJSON); err != nil {
		fmt.Printf("failed to parse JSON response: %v\n", err)
	}
	var parseResult *T
	if v, ok := respJSON["result"].(map[string]interface{}); ok {
		b, _ := json.Marshal(v)
		json.Unmarshal(b, &parseResult)
	}
	bke := BitkubError{}.ErrorFromCode(int(respJSON["error"].(float64)))
	if v, ok := respJSON["message"].(string); ok {
		bke.Description = v
	}
	r := ResponseJSON[T]{
		Error:  bke,
		Result: parseResult,
	}
	return r
}

type WalletBalance map[string]Balance
type AmountType string
type ActionType string

const (
	LimitAmount  AmountType = "limit"
	AllAvailable AmountType = "all_available"
	Percent      AmountType = "percent"
)
const (
	BuyActionType  ActionType = "buy"
	SellActionType ActionType = "sell"
)

type WebhookParams struct {
	Symbol     string     `json:"symbol"`
	Amount     float64    `json:"amount"`
	Price      float64    `json:"price"`
	Action     ActionType `json:"action"`
	AmountType AmountType `json:"amount_type"`
}

type Balance struct {
	Available float64 `json:"available"`
	Reserved  float64 `json:"reserved"`
}

type Order struct {
	ID              int64   `json:"id"`
	Hash            string  `json:"hash"`
	Type            string  `json:"typ"`
	SpendingAmount  float64 `json:"amt"`
	Rate            float64 `json:"rat"`
	Fee             float64 `json:"fee"`
	FeeCreditUsed   float64 `json:"cre"`
	AmountToReceive float64 `json:"rec"`
	Timestamp       int64   `json:"ts"`
}

type ResponseJSON[T any] struct {
	Error  *BitkubError
	Result *T `json:"result"`
}

type BitkubError struct {
	Code        int
	Description string
}

func (BitkubError) ErrorFromCode(code int) *BitkubError {
	description := ""
	switch code {
	case 1:
		description = "Invalid JSON payload"
	case 2:
		description = "Missing X-BTK-APIKEY"
	case 3:
		description = "Invalid API key"
	case 4:
		description = "API pending for activation"
	case 5:
		description = "IP not allowed"
	case 6:
		description = "Missing / invalid signature"
	case 7:
		description = "Missing timestamp"
	case 8:
		description = "Invalid timestamp"
	case 9:
		description = "Invalid user"
	case 10:
		description = "Invalid parameter"
	case 11:
		description = "Invalid symbol"
	case 12:
		description = "Invalid amount"
	case 13:
		description = "Invalid rate"
	case 14:
		description = "Improper rate"
	case 15:
		description = "Amount too low"
	case 16:
		description = "Failed to get balance"
	case 17:
		description = "Wallet is empty"
	case 18:
		description = "Insufficient balance"
	case 19:
		description = "Failed to insert order into db"
	case 20:
		description = "Failed to deduct balance"
	case 21:
		description = "Invalid order for cancellation"
	case 22:
		description = "Invalid side"
	case 23:
		description = "Failed to update order status"
	case 24:
		description = "Invalid order for lookup"
	case 25:
		description = "KYC level 1 is required to proceed"
	case 30:
		description = "Limit exceeds"
	case 40:
		description = "Pending withdrawal exists"
	case 41:
		description = "Invalid currency for withdrawal"
	case 42:
		description = "Address is not in whitelist"
	case 43:
		description = "Failed to deduct crypto"
	case 44:
		description = "Failed to create withdrawal record"
	case 45:
		description = "Nonce has to be numeric"
	case 46:
		description = "Invalid nonce"
	case 47:
		description = "Withdrawal limit exceeds"
	case 48:
		description = "Invalid bank account"
	case 49:
		description = "Bank limit exceeds"
	case 50:
		description = "Pending withdrawal exists"
	case 51:
		description = "Withdrawal is under maintenance"
	case 52:
		description = "Invalid permission"
	case 53:
		description = "Invalid internal address"
	case 54:
		description = "Address has been deprecated"
	case 55:
		description = "Cancel only mode"
	case 90:
		description = "Server error (please contact support)"
	case 404:
		description = "Not Found"
	default:
		return nil
	}
	e := BitkubError{Code: code, Description: description}
	return &e
}

func Round(val float64, precision int) float64 {
	return math.Round(val*(math.Pow10(precision))) / math.Pow10(precision)
}

// http://localhost:8080/tradingview-webhook
// {
//     "symbol": "IOST",
//     "action": "buy",
//     "price": 1,
//     "amount": 10,
//     "amount_type": "percent" // limit,all_available,percent
// }
