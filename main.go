package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

var (
	baseCfg       aws.Config
	defaultClient *sesv2.Client
)

// sesClientFor returns a client for the given region, or the default client.
func sesClientFor(region string) *sesv2.Client {
	if region == "" {
		return defaultClient
	}
	cfg := baseCfg.Copy()
	cfg.Region = region
	return sesv2.NewFromConfig(cfg)
}

func main() {
	var err error
	baseCfg, err = config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	defaultClient = sesv2.NewFromConfig(baseCfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/pools", listPools)              // GET /pools
	mux.HandleFunc("/pools/", poolRouter)            // GET|POST /pools/{name}/ips
	mux.HandleFunc("/identities/", identityRouter)   // GET|PUT /identities/{identity}/configset
	mux.HandleFunc("/configsets", listConfigSets)    // GET /configsets

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// GET /pools?region=us-east-1&configset=my-config
//
// region   (optional) — query a specific AWS region instead of the default.
// configset (optional) — return only the pool bound to this configuration set.
func listPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	region := q.Get("region")
	configSet := q.Get("configset")
	c := sesClientFor(region)

	// When a config set is specified, look up its bound sending pool directly.
	if configSet != "" {
		csOut, err := c.GetConfigurationSet(context.TODO(), &sesv2.GetConfigurationSetInput{
			ConfigurationSetName: aws.String(configSet),
		})
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var poolName string
		if csOut.DeliveryOptions != nil && csOut.DeliveryOptions.SendingPoolName != nil {
			poolName = *csOut.DeliveryOptions.SendingPoolName
		}
		writeJSON(w, map[string]any{
			"pools":     []string{poolName},
			"configset": configSet,
			"region":    effectiveRegion(c),
		})
		return
	}

	// No configset filter — list all pools in the region.
	out, err := c.ListDedicatedIpPools(context.TODO(), &sesv2.ListDedicatedIpPoolsInput{})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"pools":  out.DedicatedIpPools,
		"region": effectiveRegion(c),
	})
}

// effectiveRegion returns the region the client is configured to use.
func effectiveRegion(c *sesv2.Client) string {
	return c.Options().Region
}

// Routes /pools/{name}/ips
func poolRouter(w http.ResponseWriter, r *http.Request) {
	// expected path: /pools/{name}/ips
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[2] != "ips" {
		http.NotFound(w, r)
		return
	}
	poolName := parts[1]

	switch r.Method {
	case http.MethodGet:
		listIPs(w, r, poolName)
	case http.MethodPost:
		addIP(w, r, poolName)
	default:
		writeError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /pools/{name}/ips?region=us-east-1
// Returns all dedicated IPs assigned to the given pool.
func listIPs(w http.ResponseWriter, r *http.Request, pool string) {
	c := sesClientFor(r.URL.Query().Get("region"))
	out, err := c.GetDedicatedIps(context.TODO(), &sesv2.GetDedicatedIpsInput{
		PoolName: aws.String(pool),
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"pool": pool, "ips": out.DedicatedIps, "region": effectiveRegion(c)})
}

// POST /pools/{name}/ips
// Body: {"ip": "1.2.3.4", "region": "us-east-1"}
// Moves an existing dedicated IP in your account into the specified pool.
func addIP(w http.ResponseWriter, r *http.Request, pool string) {
	var req struct {
		IP     string `json:"ip"`
		Region string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IP == "" {
		writeError(w, `invalid body, expected {"ip":"x.x.x.x","region":"us-east-1"}`, http.StatusBadRequest)
		return
	}

	c := sesClientFor(req.Region)
	_, err := c.PutDedicatedIpInPool(context.TODO(), &sesv2.PutDedicatedIpInPoolInput{
		Ip:                  aws.String(req.IP),
		DestinationPoolName: aws.String(pool),
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"message": "IP moved to pool", "ip": req.IP, "pool": pool, "region": effectiveRegion(c)})
}

// GET /configsets?region=us-east-1
// Returns all configuration set names in the region, handling pagination automatically.
func listConfigSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	c := sesClientFor(r.URL.Query().Get("region"))
	paginator := sesv2.NewListConfigurationSetsPaginator(c, &sesv2.ListConfigurationSetsInput{})
	all := []string{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.TODO())
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		all = append(all, page.ConfigurationSets...)
	}
	writeJSON(w, map[string]any{"configsets": all, "region": effectiveRegion(c)})
}

// Routes /identities/{identity}/configset
func identityRouter(w http.ResponseWriter, r *http.Request) {
	// expected path: /identities/{identity}/configset
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[2] != "configset" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		getIdentityConfigSet(w, r, parts[1])
	case http.MethodPut:
		setIdentityConfigSet(w, r, parts[1])
	default:
		writeError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /identities/{identity}/configset?region=us-east-1
// Returns the default configuration set bound to the given email identity.
func getIdentityConfigSet(w http.ResponseWriter, r *http.Request, identity string) {
	c := sesClientFor(r.URL.Query().Get("region"))
	out, err := c.GetEmailIdentity(context.TODO(), &sesv2.GetEmailIdentityInput{
		EmailIdentity: aws.String(identity),
	})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var configSet string
	if out.ConfigurationSetName != nil {
		configSet = *out.ConfigurationSetName
	}
	writeJSON(w, map[string]any{
		"identity":   identity,
		"configset":  configSet,
		"region":     effectiveRegion(c),
	})
}

// PUT /identities/{identity}/configset
// Body: {"configset": "my-config", "region": "us-east-1"}
// Pass empty string for configset to detach the current default.
func setIdentityConfigSet(w http.ResponseWriter, r *http.Request, identity string) {
	var req struct {
		ConfigSet string `json:"configset"`
		Region    string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, `invalid body, expected {"configset":"name","region":"us-east-1"}`, http.StatusBadRequest)
		return
	}

	c := sesClientFor(req.Region)
	input := &sesv2.PutEmailIdentityConfigurationSetAttributesInput{
		EmailIdentity: aws.String(identity),
	}
	if req.ConfigSet != "" {
		input.ConfigurationSetName = aws.String(req.ConfigSet)
	}
	if _, err := c.PutEmailIdentityConfigurationSetAttributes(context.TODO(), input); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"identity":  identity,
		"configset": req.ConfigSet,
		"region":    effectiveRegion(c),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
