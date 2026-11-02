package membership

import "crypto/ed25519"

// Signer signs log entries without handing out the private key. A file key, a hardware module or a cloud
// key service can each sit behind it.
type Signer interface {
	Public() ed25519.PublicKey
	Sign(msg []byte) ([]byte, error)
}

// KeySigner signs with a key held in memory (the control plane's own key, loaded from its sealed storage).
type KeySigner struct{ Key ed25519.PrivateKey }

func (k KeySigner) Public() ed25519.PublicKey { return k.Key.Public().(ed25519.PublicKey) }

func (k KeySigner) Sign(msg []byte) ([]byte, error) { return ed25519.Sign(k.Key, msg), nil }
