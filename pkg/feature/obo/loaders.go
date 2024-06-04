package obo

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func AlertmanagerDataValue(f *feature.Feature) error {
	deadmansnitchSecret := &corev1.Secret{}
	err := f.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: f.Spec.MonNamespace,
		Name:      "redhat-rhods-deadmanssnitch",
	}, deadmansnitchSecret)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting secret %s: %w", "redhat-rhods-deadmanssnitch", err)
		}
	}
	f.Spec.AlertmanagerData.DeadManSnitchURL = string(deadmansnitchSecret.Data["SNITCH_URL"])

	// Get the SMTP details
	smtpSecret := &corev1.Secret{}
	err = f.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: f.Spec.MonNamespace,
		Name:      "redhat-rhods-smtp",
	}, smtpSecret)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting secret %s: %w", "redhat-rhods-smtp", err)
		}
	}
	f.Spec.AlertmanagerData.SMTPHost = string(smtpSecret.Data["host"])
	f.Spec.AlertmanagerData.SMTPPort = string(smtpSecret.Data["port"])
	f.Spec.AlertmanagerData.SMTPUSER = string(smtpSecret.Data["username"])
	// f.Spec.AlertmanagerData.SMTPPSD = string(smtpSecret.Data["password"])

	// Get the notification email
	addOnUser := &corev1.Secret{}
	err = f.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: f.Spec.MonNamespace,
		Name:      "addon-managed-odh-parameters",
	}, addOnUser)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting secret %s: %w", "addon-managed-odh-parameters", err)
		}
	}
	f.Spec.AlertmanagerData.NotificationEmail = string(addOnUser.Data["notification-email"])

	// Get the domain
	domain, errDomain := cluster.GetDomain(f.Client)
	if errDomain != nil {
		return fmt.Errorf("failed to fetch OpenShift domain for config AlterManager: %w", errDomain)
	}
	if strings.Contains(domain, "devshift.org") {
		f.Spec.AlertmanagerData.SMTPDoamin = "@rhmw.io"
	}
	f.Spec.AlertmanagerData.SMTPDoamin = "@devshift.net"

	// Receiver for PagerDuty
	if strings.Contains(domain, "aisrhods") {
		f.Spec.AlertmanagerData.PagerDutyReceiver = "alerts-sink"
	}
	f.Spec.AlertmanagerData.PagerDutyReceiver = "PagerDuty"

	// PagerDuty Key
	pagerDutySecret := &corev1.Secret{}
	err = f.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: f.Spec.MonNamespace,
		Name:      "redhat-rhods-pagerduty",
	}, pagerDutySecret)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting secret %s: %w", "redhat-rhods-pagerduty", err)
		}
	}
	f.Spec.AlertmanagerData.PagerDutyKey = string((pagerDutySecret.Data["token"]))

	// Config for email
	AlertManagerConfigMap := &corev1.ConfigMap{}
	err = f.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: f.Spec.MonNamespace,
		Name:      "rhoai-alertmanager-configmap",
	}, AlertManagerConfigMap)
	if err != nil {
		return fmt.Errorf("error getting configmap %s: %w", "rhoai-alertmanager-configmap", err)
	}
	f.Spec.AlertmanagerData.EmailBody  = AlertManagerConfigMap.Data["email-rhoai-body.tmpl"]
	f.Spec.AlertmanagerData.EmailSubject = AlertManagerConfigMap.Data["email-rhoai-subject.tmpl"]

	return nil
}
