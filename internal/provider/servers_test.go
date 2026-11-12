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

// fakeServers is Cloud starting servers in AWS: what it was sent, and what
// it ended.
type fakeServers struct {
	mu      sync.Mutex
	servers map[string]map[string]any
	sent    []map[string]any
	deleted []string
}

func (f *fakeServers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/cloud/servers/")
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/cloud/servers":
		f.sent = append(f.sent, in)
		sv := map[string]any{
			"id": "sv9", "name": in["name"], "rack_id": "rk1", "origin": in["origin"], "status": "pending",
			"aws_account_id": in["aws_account_id"], "region": "sa-east-1", "instance_type": "t3.small",
			"disk_gb": 30, "instance_id": "i-0abc",
		}
		if it, ok := in["instance_type"].(string); ok && it != "" {
			sv["instance_type"] = it
		}
		f.servers["sv9"] = sv
		reply(201, sv)
	case r.Method == "GET" && f.servers[id] != nil:
		reply(200, f.servers[id])
	case r.Method == "DELETE" && f.servers[id] != nil:
		f.deleted = append(f.deleted, id)
		delete(f.servers, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		reply(404, map[string]string{"error": "No such thing."})
	}
}

// A server in AWS: started by Cloud, with no join command to run; its
// account's region and type filled in; ended when destroyed.
func TestAServerInAWS(t *testing.T) {
	f := &fakeServers{servers: map[string]map[string]any{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: cloudProvider(srv.URL) + `
resource "yet_cloud_server" "ec2" {
  name           = "ec2-1"
  aws_account_id = "aws_1"
  instance_type  = "t4g.medium"
}
`,
			ConfigStateChecks: []statecheck.StateCheck{
				statecheck.ExpectKnownValue("yet_cloud_server.ec2", tfjsonpath.New("origin"), knownvalue.StringExact("aws")),
				statecheck.ExpectKnownValue("yet_cloud_server.ec2", tfjsonpath.New("instance_id"), knownvalue.StringExact("i-0abc")),
				statecheck.ExpectKnownValue("yet_cloud_server.ec2", tfjsonpath.New("region"), knownvalue.StringExact("sa-east-1")),
				statecheck.ExpectKnownValue("yet_cloud_server.ec2", tfjsonpath.New("instance_type"), knownvalue.StringExact("t4g.medium")),
				statecheck.ExpectKnownValue("yet_cloud_server.ec2", tfjsonpath.New("join_command"), knownvalue.StringExact("")),
			},
		}},
	})
	if len(f.sent) != 1 || f.sent[0]["origin"] != "aws" || f.sent[0]["aws_account_id"] != "aws_1" {
		t.Errorf("started in aws_1: %+v", f.sent)
	}
	if len(f.deleted) != 1 {
		t.Errorf("ended when destroyed: %+v", f.deleted)
	}
}
