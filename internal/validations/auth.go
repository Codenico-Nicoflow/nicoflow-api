package validations

import (
	"net"
	"regexp"
	"strings"
	"unicode"

	"github.com/go-playground/validator/v10"

	"github.com/nicoflow/nicoflow-api/internal/response"
)

func PasswordValidator(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	if len(password) < 8 {
		return false
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool

	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		case regexp.MustCompile(`[\W_]`).MatchString(string(c)):
			hasSpecial = true
		}
	}

	return hasUpper && hasLower && hasDigit && hasSpecial
}

func FormatValidationError(err error) (string, string) {
	errs, ok := err.(validator.ValidationErrors)
	if ok {
		for _, e := range errs {
			switch e.Tag() {
			case "required":
				return response.ErrRequired, strings.ToLower(e.Field()) + " is required"
			case "email":
				return response.ErrInvalidEmail, strings.ToLower(e.Field()) + " is not a valid email"
			case "strong_password":
				return response.ErrWeakPassword, strings.ToLower(e.Field()) + " is not a strong password"
			}
		}
	}
	return response.ErrInvalidInput, err.Error()
}

func VerifyEmailDomain(email string) bool {
	if !strings.Contains(email, "@") {
		return false
	}
	domain := strings.Split(email, "@")[1]
	return hasMXRecord(domain)
}

func hasMXRecord(domain string) bool {
	mxRecords, err := net.LookupMX(domain)
	return err == nil && len(mxRecords) > 0
}
