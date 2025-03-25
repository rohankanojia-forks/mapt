package keypair

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/pulumi/pulumi-tls/sdk/v5/go/tls"
	"golang.org/x/crypto/ssh"
)

type KeyPairRequest struct {
	Name string
}

type KeyPairResources struct {
	PublicKey  string
	PrivateKey *tls.PrivateKey
}

func GenerateSSHKeyPair(bits int) (privateKeyPEM string, publicKeySSH string, err error) {
	// Generate a new RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", err
	}

	// Convert private key to PEM format
	privateKeyBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyPEM = string(pem.EncodeToMemory(&privateKeyBlock))

	// Generate public key in SSH authorized_keys format
	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	publicKeySSH = string(ssh.MarshalAuthorizedKey(pub))

	return privateKeyPEM, publicKeySSH, nil
}

// Helper function to marshal private key (safe export)
func x509MarshalPKCS1PrivateKey(key *rsa.PrivateKey) []byte {
	return x509.MarshalPKCS1PrivateKey(key)
}
