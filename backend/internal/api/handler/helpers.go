package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

// validate is the shared validator with CPM's custom rules registered.
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	// "domains" validates a space/comma separated list where each entry is a
	// valid FQDN or host:port.
	_ = v.RegisterValidation("domains", func(fl validator.FieldLevel) bool {
		fields := strings.FieldsFunc(fl.Field().String(), func(r rune) bool {
			return r == ' ' || r == ',' || r == '\t'
		})
		if len(fields) == 0 {
			return false
		}
		for _, d := range fields {
			if v.Var(d, "fqdn") != nil && v.Var(d, "hostname_port") != nil {
				return false
			}
		}
		return true
	})
	return v
}

func getURLParamInt(r *http.Request, varName string) (int, error) {
	required := true
	defaultValue := 0
	paramStr := chi.URLParam(r, varName)
	var err error
	var paramInt int

	if paramStr == "" && required {
		return 0, fmt.Errorf("%v was not supplied in the request", varName)
	} else if paramStr == "" {
		return defaultValue, nil
	}

	if paramInt, err = strconv.Atoi(paramStr); err != nil {
		return 0, fmt.Errorf("%v is not a valid number", varName)
	}

	return paramInt, nil
}
