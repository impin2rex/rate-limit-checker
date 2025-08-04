# Install

`git clone https://github.com/impin2rex/rate-limit-checker && cd rate-limit-checker && go build rate.go`

or

`go get github.com/impin2rex/rate-limit-checker`

# Usage
```
./rate \
  --url "https://rpc.shyft.to?api_key=YOUR_API_KEY" \
  --method POST \
  --requests 500 \
  --body '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "getAccountInfo",
    "params": [
        "68q1YeY3QJoL3DF3umVKkCFARYh931sQTbZbRtYthGu9",
        {
            "encoding": "jsonParsed",
            "commitment": "processed"
        }
    ]
  }' \
  --header "Content-Type:application/json"
```