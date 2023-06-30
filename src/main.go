package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	APIAccessKey    = "" // Your API request authorization key from the Call Password section.
	APISignatureKey = "" // Your API request signing key from the Call Password section.
	Timeout         = 60 // Value in seconds for call verification. Possible values: min 30, max 120, default 60.
)

type RequestBody struct {
	ClientNumber string `json:"clientNumber"`
	CallbackLink string `json:"callbackLink"`
	Timeout      int    `json:"timeout"`
}

type Response struct {
	Status string `json:"status"`
	Data   struct {
		Result      string `json:"result"`
		CallDetails struct {
			CallID             string `json:"callId"`
			CallbackLink       string `json:"callbackLink"`
			ClientNumber       string `json:"clientNumber"`
			ConfirmationNumber string `json:"confirmationNumber"`
			IsMnp              bool   `json:"isMnp"`
			OperatorName       string `json:"operatorName"`
			OperatorNameMnp    any    `json:"operatorNameMnp"`
			RegionName         string `json:"regionName"`
			QrCodeURI          string `json:"qrCodeUri"`
			UserData           any    `json:"userData"`
		} `json:"callDetails"`
	} `json:"data"`
}

type Data struct {
	Timestamp int64 `json:"timestamp"`
	Flag      bool  `json:"flag"`
}

type CallbackResponse struct {
	CallId string `json:"callId"`
}

func getAuthToken(requestMethod string, time int64, accessKey string, params string, signatureKey string) string {
	hash := strings.Join([]string{requestMethod, strconv.FormatInt(time, 10), accessKey, params, signatureKey}, "\n")
	return accessKey + strconv.FormatInt(time, 10) + fmt.Sprintf("%x", sha256.Sum256([]byte(hash)))
}

func main() {

	// Open the my.db data file in your current directory.
	// It will be created if it doesn't exist.
	db, err := bolt.Open("my.db", 0600, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")

		if r.Method == http.MethodPost {
			switch r.FormValue("action") {
			case "start":
				// Start the call verification process.
				requestBody := RequestBody{
					ClientNumber: r.FormValue("phoneNumber"),
					CallbackLink: fmt.Sprintf("http://%s/?action=callback", r.Host),
					Timeout:      Timeout,
				}

				requestJSON, _ := json.Marshal(requestBody)

				token := getAuthToken("call-verification/start-inbound-call-waiting", time.Now().Unix(), APIAccessKey, string(requestJSON), APISignatureKey)
				client := &http.Client{}

				req, _ := http.NewRequest("POST", "https://api.new-tel.net/call-verification/start-inbound-call-waiting", bytes.NewBuffer(requestJSON))

				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				resp, _ := client.Do(req)

				defer resp.Body.Close()

				responseBody, _ := ioutil.ReadAll(resp.Body)

				var decodedResponse Response
				_ = json.Unmarshal(responseBody, &decodedResponse)

				if decodedResponse.Status == "success" {
					callDetailsJSON, _ := json.Marshal(decodedResponse.Data.CallDetails)

					data, _ := json.Marshal(map[string]interface{}{
						"timestamp": time.Now().Add(time.Duration(Timeout) * time.Second).Unix(),
						"flag":      false,
					})

					_ = db.Update(func(tx *bolt.Tx) error {
						bucket, _ := tx.CreateBucketIfNotExists([]byte("mybucket"))
						_ = bucket.Put([]byte(decodedResponse.Data.CallDetails.CallID), data)

						return nil
					})

					w.Header().Set("Content-Type", "application/json")

					_, _ = w.Write(callDetailsJSON)
				}
			case "check":
				// Check the call confirmation status.
				_ = db.View(func(tx *bolt.Tx) error {

					bucket := tx.Bucket([]byte("mybucket"))
					value := bucket.Get([]byte(r.FormValue("callId")))

					var data Data
					_ = json.Unmarshal(value, &data)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"timeout": data.Timestamp - time.Now().Unix(),
						"flag":    data.Flag,
					})

					return nil
				})
			case "callback":
				// Process callback from call verification.
				var timestamp int64

				body, _ := ioutil.ReadAll(r.Body)
				defer r.Body.Close()

				var jsonData CallbackResponse
				_ = json.Unmarshal(body, &jsonData)
				_ = db.View(func(tx *bolt.Tx) error {
					bucket := tx.Bucket([]byte("mybucket"))
					value := bucket.Get([]byte(jsonData.CallId))

					var data Data
					_ = json.Unmarshal(value, &data)
					timestamp = data.Timestamp

					return nil
				})

				callbackData, _ := json.Marshal(map[string]interface{}{
					"timestamp": timestamp,
					"flag":      true,
				})

				_ = db.Update(func(tx *bolt.Tx) error {
					bucket, _ := tx.CreateBucketIfNotExists([]byte("mybucket"))
					_ = bucket.Put([]byte(jsonData.CallId), callbackData)
					return nil
				})
			}
		}
	})

	err = http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal(err)
	}
}
