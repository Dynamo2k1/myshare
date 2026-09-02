package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/dynamo2k1/myshare/internal/netinfo"
)

// ensureTLSCert returns paths to a cert/key pair in <dataDir>/certs, generating
// a self-signed pair (valid for localhost and every detected LAN IP) if none
// exists or the existing one has expired. Self-signed means devices show a
// one-time trust prompt; that is the documented cost of full clipboard/image
// support over LAN.
func (a *App) ensureTLSCert() (certPath, keyPath string, err error) {
	dir := filepath.Join(a.Layout.Root, "certs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	certPath = filepath.Join(dir, "myshare.crt")
	keyPath = filepath.Join(dir, "myshare.key")

	if fresh, _ := certStillValid(certPath); fresh {
		return certPath, keyPath, nil
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "MyShare (self-signed)", Organization: []string{"MyShare"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(2, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "myshare.local"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	for _, ip := range netinfo.LANIPs() {
		if parsed := net.ParseIP(ip); parsed != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, parsed)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", keyDER, 0o600); err != nil {
		return "", "", err
	}
	a.Log.Info("generated self-signed TLS certificate", "path", certPath,
		"valid_until", tmpl.NotAfter.Format("2006-01-02"))
	return certPath, keyPath, nil
}

func certStillValid(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return false, nil
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return false, err
	}
	return time.Now().Before(c.NotAfter.Add(-24 * time.Hour)), nil
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}
