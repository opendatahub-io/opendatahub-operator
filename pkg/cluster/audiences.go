package cluster

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	authentication "k8s.io/api/authentication/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// Default value of audiences for DSCI.SM.auth.
	defaultAudiences = []string{"https://kubernetes.default.svc"}
)

func isDefaultAudiences(specAudiences *[]string) bool {
	return specAudiences == nil || reflect.DeepEqual(*specAudiences, defaultAudiences)
}

type TokenSupplier func() (string, error)

// GetEffectiveClusterAudiences returns the audiences defined for the cluster
// falling back to the default audiences in case of errors.
func GetEffectiveClusterAudiences(cli client.Client, log logr.Logger, specAudiences *[]string, getTokenFunc TokenSupplier) []string {
	if isDefaultAudiences(specAudiences) {
		return fetchClusterAudiences(cli, log, getTokenFunc)
	}
	return *specAudiences
}

func fetchClusterAudiences(cli client.Client, log logr.Logger, getTokenFunc TokenSupplier) []string {
	token, err := getTokenFunc()
	if err != nil {
		log.Error(err, "Error getting token, using default audiences")
		return defaultAudiences
	}

	tokenReview := &authentication.TokenReview{
		Spec: authentication.TokenReviewSpec{
			Token: token,
		},
	}

	if err = cli.Create(context.Background(), tokenReview, &client.CreateOptions{}); err != nil {
		log.Error(err, "Error creating TokenReview, using default audiences")
		return defaultAudiences
	}

	if tokenReview.Status.Error != "" || !tokenReview.Status.Authenticated {
		log.Error(fmt.Errorf(tokenReview.Status.Error), "Error with token review authentication status, using default audiences")
		return defaultAudiences
	}

	return tokenReview.Status.Audiences
}
