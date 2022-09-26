package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

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
