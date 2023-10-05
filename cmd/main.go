package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/boltdb/bolt"
	"io"
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

func getAuthToken(requestMethod, accessKey, params, signatureKey string, time int64) string {
	hash := strings.Join([]string{requestMethod, strconv.FormatInt(time, 10), accessKey, params, signatureKey}, "\n")
	return accessKey + strconv.FormatInt(time, 10) + fmt.Sprintf("%x", sha256.Sum256([]byte(hash)))
}

func main() {
	// Open my.db data file in your current directory.
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
				body, err := json.Marshal(map[string]interface{}{
					"clientNumber": r.FormValue("phoneNumber"),
					"callbackLink": "http://" + r.Host + "/?action=callback",
					"timeout":      Timeout,
				})
				if err != nil {
					log.Fatal(err)
				}

				token := getAuthToken("call-verification/start-inbound-call-waiting", APIAccessKey, string(body), APISignatureKey, time.Now().Unix())
				client := &http.Client{}

				req, err := http.NewRequest(http.MethodPost, "https://api.new-tel.net/call-verification/start-inbound-call-waiting", bytes.NewBuffer(body))

				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				respBody, err := io.ReadAll(resp.Body)

				var decodedResp Response
				err = json.Unmarshal(respBody, &decodedResp)

				if decodedResp.Status == "success" {
					callDetailsJSON, err := json.Marshal(decodedResp.Data.CallDetails)
					if err != nil {
						log.Fatal(err)
					}

					data, err := json.Marshal(map[string]interface{}{
						"timestamp": time.Now().Add(time.Duration(Timeout) * time.Second).Unix(),
						"flag":      false,
					})

					err = db.Update(func(tx *bolt.Tx) error {
						bucket, err := tx.CreateBucketIfNotExists([]byte("myBucket"))
						err = bucket.Put([]byte(decodedResp.Data.CallDetails.CallID), data)
						return err
					})

					w.Header().Set("Content-Type", "application/json")

					_, err = w.Write(callDetailsJSON)
				}
			case "check":
				// Check the call confirmation status.
				err = db.View(func(tx *bolt.Tx) error {
					bucket := tx.Bucket([]byte("myBucket"))
					value := bucket.Get([]byte(r.FormValue("callId")))

					var data Data
					err = json.Unmarshal(value, &data)
					err = json.NewEncoder(w).Encode(map[string]interface{}{
						"timeout": data.Timestamp - time.Now().Unix(),
						"flag":    data.Flag,
					})

					return err
				})
			case "callback":
				// Process callback from call verification.
				var timestamp int64

				body, err := io.ReadAll(r.Body)
				if err != nil {
					log.Fatal(err)
				}

				var jsonData CallbackResponse
				err = json.Unmarshal(body, &jsonData)
				err = db.View(func(tx *bolt.Tx) error {
					bucket := tx.Bucket([]byte("myBucket"))
					value := bucket.Get([]byte(jsonData.CallId))

					var data Data
					err = json.Unmarshal(value, &data)
					timestamp = data.Timestamp

					return err
				})

				callbackData, err := json.Marshal(map[string]interface{}{
					"timestamp": timestamp,
					"flag":      true,
				})

				err = db.Update(func(tx *bolt.Tx) error {
					bucket, err := tx.CreateBucketIfNotExists([]byte("myBucket"))
					err = bucket.Put([]byte(jsonData.CallId), callbackData)
					return err
				})
			}
		}
	})

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
