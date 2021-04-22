package prometheus

import (
	"bytes"
	"fmt"
	"text/template"
	"time"

	"github.com/go-playground/validator/v10"
	prommodel "github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/rulefmt"
	promqlparser "github.com/prometheus/prometheus/promql/parser"
)

// CustomSLI reprensents an SLI with custom error and total expressions.
type CustomSLI struct {
	ErrorQuery string `validate:"required,prom_expr"`
	TotalQuery string `validate:"required,prom_expr"`
}

// AlertMeta is the metadata of an alert settings.
type AlertMeta struct {
	Disable     bool
	Name        string            `validate:"required_if_enabled"`
	Labels      map[string]string `validate:"dive,keys,prom_label_key,endkeys,required,prom_label_value"`
	Annotations map[string]string `validate:"dive,keys,prom_annot_key,endkeys,required"`
}

// SLO represents a service level objective configuration.
type SLO struct {
	ID               string    `validate:"required"`
	Name             string    `validate:"required"`
	Service          string    `validate:"required"`
	SLI              CustomSLI `validate:"required"`
	TimeWindow       time.Duration
	Objective        float64           `validate:"gt=0,lte=100"`
	Labels           map[string]string `validate:"dive,keys,prom_label_key,endkeys,required,prom_label_value"`
	PageAlertMeta    AlertMeta
	WarningAlertMeta AlertMeta
}

// Validate validates the SLO.
func (s SLO) Validate() error {
	return modelSpecValidate.Struct(s)
}

// GetSLIErrorMetric returns the SLI error metric.
func (s SLO) GetSLIErrorMetric(window time.Duration) string {
	return fmt.Sprintf(sliErrorMetricFmt, timeDurationToPromStr(window))
}

// GetSLOIDPromLabels returns the ID labels of an SLO, these can be used to identify
// an SLO recorded metrics and alerts.
func (s SLO) GetSLOIDPromLabels() map[string]string {
	return map[string]string{
		sloIDLabelName:      s.ID,
		sloNameLabelName:    s.Name,
		sloServiceLabelName: s.Service,
	}
}

var modelSpecValidate = func() *validator.Validate {
	v := validator.New()

	// Mor information on prometheus validators logic: https://github.com/prometheus/prometheus/blob/df80dc4d3970121f2f76cba79050983ffb3cdbb0/pkg/rulefmt/rulefmt.go#L188-L208
	mustRegisterValidation(v, "prom_expr", validatePromExpression)
	mustRegisterValidation(v, "prom_label_key", validatePromLabelKey)
	mustRegisterValidation(v, "prom_label_value", validatePromLabelValue)
	mustRegisterValidation(v, "prom_annot_key", validatePromAnnotKey)
	mustRegisterValidation(v, "required_if_enabled", validateRequiredEnabledAlertName)

	return v
}()

// mustRegisterValidation is a helper so we panic on start if we can't register a validator.
func mustRegisterValidation(v *validator.Validate, tag string, fn validator.Func) {
	err := v.RegisterValidation(tag, fn)
	if err != nil {
		panic(err)
	}
}

// validatePromAnnotKey implements validator.CustomTypeFunc by validating
// a prometheus annotation key.
func validatePromAnnotKey(fl validator.FieldLevel) bool {
	k, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}

	return prommodel.LabelName(k).IsValid()
}

// validatePromLabel implements validator.CustomTypeFunc by validating
// a prometheus label key.
func validatePromLabelKey(fl validator.FieldLevel) bool {
	k, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}

	return prommodel.LabelName(k).IsValid() && k != prommodel.MetricNameLabel
}

// validatePromLabelValue implements validator.CustomTypeFunc by validating
// a prometheus label value.
func validatePromLabelValue(fl validator.FieldLevel) bool {
	v, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}

	return prommodel.LabelValue(v).IsValid()
}

var promExprTplAllowedFakeData = map[string]string{
	"window": "1m",
}

// validatePromExpression implements validator.CustomTypeFunc by validating
// a prometheus expression.
func validatePromExpression(fl validator.FieldLevel) bool {
	expr, ok := fl.Field().Interface().(string)
	if !ok {
		return false
	}

	// The expressions set by users can have some allowed templated data
	// we are rendering the expression with fake data so prometheus can
	// have a final expr and check if is correct.
	tpl, err := template.New("expr").Parse(expr)
	if err != nil {
		return false
	}

	var tplB bytes.Buffer
	err = tpl.Execute(&tplB, promExprTplAllowedFakeData)
	if err != nil {
		return false
	}

	_, err = promqlparser.ParseExpr(tplB.String())
	return err == nil
}

func validateRequiredEnabledAlertName(fl validator.FieldLevel) bool {
	alertMeta, ok := fl.Parent().Interface().(AlertMeta)
	if !ok {
		return false
	}

	if alertMeta.Disable {
		return true
	}

	return alertMeta.Name != ""
}

// SLORules are the prometheus rules required by an SLO.
type SLORules struct {
	RecordingRules []rulefmt.Rule
	AlertRules     []rulefmt.Rule
}