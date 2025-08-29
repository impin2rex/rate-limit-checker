# Install

`git clone https://github.com/impin2rex/rate-limit-checker && cd rate-limit-checker && go build rate.go`

or

`go install github.com/impin2rex/rate-limit-checker@latest`

# Usage
```bash
Usage of rate:
  -X string
        method (default "GET")
  -u string
        url
  -r int
        rps (default 200)
  -t int
        duration (default 10)
  -w int
        concurrent workers count (default 50)
  -d string
        request body (stringified json) # optional
  -h string
        headers in format 'Key:Value,Key2:Value2' # optional
  
```

## Example
```
rate-limit-checker \
  --u "https://rpc.shyft.to?api_key=YOUR_API_KEY" \
  --X POST \
  --r 1000 \
  --t 30 \
  --w 300 \
  --d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "getSlot"
  }' \
  --h "Content-Type:application/json"
```

## Verbose (to see debug log)
```bash
--loglevel debug
```