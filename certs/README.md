# TLS certificates

Do not commit private keys. Generate local material:

```bash
openssl req -x509 -newkey rsa:4096 -sha256 -days 365 -nodes \
  -subj "/CN=kaf-mirror-ca" \
  -keyout ca-key.pem -out ca.pem

openssl req -newkey rsa:4096 -nodes \
  -subj "/CN=localhost" \
  -keyout server-key.pem -out server.csr

openssl x509 -req -in server.csr -CA ca.pem -CAkey ca-key.pem \
  -CAcreateserial -days 365 -sha256 \
  -extfile extfile.cnf -out server.pem
```

Point `server.tls.cert_file` and `server.tls.key_file` at `server.pem` and `server-key.pem`.
