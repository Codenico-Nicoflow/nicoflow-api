package hashutil

import "golang.org/x/crypto/bcrypt"

const cost = 12

// Hash returns a bcrypt hash of plain at cost 12.
func Hash(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Compare reports whether plain matches the bcrypt hash.
func Compare(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
