package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// fakeSchedules is an application's schedules in Cloud.
type fakeSchedules struct {
	mu   sync.Mutex
	one  map[string]any
	sent []map[string]any
}

func (f *fakeSchedules) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	const base = "/v1/cloud/applications/app1/schedules"
	switch {
	case r.Method == "POST" && r.URL.Path == base:
		f.sent = append(f.sent, in)
		f.one = map[string]any{"id": "sc1", "application_id": "app1", "time_zone": "", "method": "POST", "stages": []string{}}
		for k, v := range in {
			f.one[k] = v
		}
		reply(201, f.one)
	case f.one != nil && r.URL.Path == base+"/sc1" && r.Method == "GET":
		reply(200, f.one)
	case f.one != nil && r.URL.Path == base+"/sc1" && r.Method == "PATCH":
		f.sent = append(f.sent, in)
		for k, v := range in {
			f.one[k] = v
		}
		reply(200, f.one)
	case f.one != nil && r.URL.Path == base+"/sc1" && r.Method == "DELETE":
		f.one = nil
		w.WriteHeader(http.StatusNoContent)
	default:
		reply(404, map[string]string{"error": "No such thing: " + strings.TrimPrefix(r.URL.Path, "/v1")})
	}
}

// A schedule: POST unless said, on every stage unless said; changed in place.
func TestASchedule(t *testing.T) {
	f := &fakeSchedules{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	config := func(cron string) string {
		return cloudProvider(srv.URL) + `
resource "yet_cloud_application_schedule" "reconcile" {
  application_id = "app1"
  name           = "reconcile"
  cron           = "` + cron + `"
  path           = "/api/cron/reconcile"
}
`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: config("* * * * *"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_application_schedule.reconcile", tfjsonpath.New("id"), knownvalue.StringExact("sc1")),
					statecheck.ExpectKnownValue("yet_cloud_application_schedule.reconcile", tfjsonpath.New("method"), knownvalue.StringExact("POST")),
				},
			},
			{
				Config: config("*/5 * * * *"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("yet_cloud_application_schedule.reconcile", tfjsonpath.New("cron"), knownvalue.StringExact("*/5 * * * *")),
				},
			},
			{ResourceName: "yet_cloud_application_schedule.reconcile", ImportState: true, ImportStateId: "app1/sc1", ImportStateVerify: true},
		},
	})
	if len(f.sent) != 2 || f.sent[1]["cron"] != "*/5 * * * *" {
		t.Errorf("made, then changed: %+v", f.sent)
	}
	if f.one != nil {
		t.Errorf("gone when destroyed: %+v", f.one)
	}
}
