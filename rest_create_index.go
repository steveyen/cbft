//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the
//  License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing,
//  software distributed under the License is distributed on an "AS
//  IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
//  express or implied. See the License for the specific language
//  governing permissions and limitations under the License.

package cbft

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"

	"github.com/gorilla/mux"
)

type CreateIndexHandler struct {
	mgr *Manager
}

func NewCreateIndexHandler(mgr *Manager) *CreateIndexHandler {
	return &CreateIndexHandler{mgr: mgr}
}

func (h *CreateIndexHandler) RESTOpts(opts map[string]string) {
	indexTypes := []string(nil)
	for indexType, t := range PIndexImplTypes {
		indexTypes = append(indexTypes,
			"```"+indexType+"```: "+
				strings.Split(t.Description, " - ")[1])
	}
	sort.Strings(indexTypes)

	indexParams := []string(nil)
	for _, indexDesc := range indexTypes {
		indexType := strings.Split(indexDesc, "```")[1]
		t := PIndexImplTypes[indexType]
		if t.StartSample != nil {
			indexParams = append(indexParams,
				"For indexType ```"+indexType+"```"+
					", an example indexParams JSON:\n\n    "+
					IndentJSON(t.StartSample, "    ", "  "))
		} else {
			indexParams = append(indexParams,
				"For indexType ```"+indexType+"```"+
					", the indexParams can be null.")
		}
	}

	sourceTypes := []string(nil)
	for sourceType, t := range FeedTypes {
		if t.Public {
			sourceTypes = append(sourceTypes,
				"```"+sourceType+"```: "+
					strings.Split(t.Description, " - ")[1])
		}
	}
	sort.Strings(sourceTypes)

	sourceParams := []string(nil)
	for _, sourceDesc := range sourceTypes {
		sourceType := strings.Split(sourceDesc, "```")[1]
		t := FeedTypes[sourceType]
		if t.StartSample != nil {
			sourceParams = append(sourceParams,
				"For sourceType ```"+sourceType+"```"+
					", an example sourceParams JSON:\n\n    "+
					IndentJSON(t.StartSample, "    ", "  "))
		} else {
			sourceParams = append(sourceParams,
				"For sourceType ```"+sourceType+"```"+
					", the sourceParams can be null.")
		}
	}

	opts["param: indexName"] =
		"required, string, URL path parameter\n\n" +
			"The name of the to-be-created/updated index definition,\n" +
			"validated with the regular expression of ```" +
			INDEX_NAME_REGEXP + "```."
	opts["param: indexType"] =
		"required, string, form parameter\n\n" +
			"Supported indexType's:\n\n* " +
			strings.Join(indexTypes, "\n* ")
	opts["param: indexParams"] =
		"optional (depends on the value of the indexType)," +
			" string (JSON), form parameter\n\n" +
			strings.Join(indexParams, "\n\n")
	opts["param: sourceType"] =
		"required, string, form parameter\n\n" +
			"Supported sourceType's:\n\n* " +
			strings.Join(sourceTypes, "\n* ")
	opts["param: sourceName"] =
		"optional, string, form parameter"
	opts["param: sourceUUID"] =
		"optional, string, form parameter"
	opts["param: sourceParams"] =
		"optional (depends on the value of the sourceType)," +
			" string (JSON), form parameter\n\n" +
			strings.Join(sourceParams, "\n\n")
	opts["param: planParams"] =
		"optional, string (JSON), form parameter"
	opts["param: prevIndexUUID"] =
		"optional, string, form parameter\n\n" +
			"Intended for clients that want to check that they are not " +
			"overwriting the index definition updates of concurrent clients."
	opts["result on error"] =
		`non-200 HTTP error code`
	opts["result on success"] =
		`HTTP 200 with body JSON of {"status": "ok"}` // TODO: Revisit 200 code.
}

func (h *CreateIndexHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// TODO: Need more input validation (check source UUID's, etc).
	indexName := mux.Vars(req)["indexName"]
	if indexName == "" {
		showError(w, req, "rest_create_index: index name is required", 400)
		return
	}

	requestBody, err := ioutil.ReadAll(req.Body)
	if err != nil {
		showError(w, req, fmt.Sprintf("rest_create_index:"+
			" could not read request body, indexName: %s, err: %v",
			indexName, err), 400)
		return
	}

	var indexDef IndexDef
	if len(requestBody) > 0 {
		err := json.Unmarshal(requestBody, &indexDef)
		if err != nil {
			showError(w, req, fmt.Sprintf("rest_create_index:"+
				" could not unmarshal json, indexName: %s, err: %v",
				indexName, err), 400)
			return
		}
	}

	indexType := req.FormValue("indexType")
	if indexType == "" {
		indexType = indexDef.Type
	}
	if indexType == "" {
		showError(w, req, "rest_create_index: indexType is required", 400)
		return
	}

	indexParams := req.FormValue("indexParams")
	if indexParams == "" {
		indexParams = indexDef.Params
	}

	sourceType := req.FormValue("sourceType")
	if sourceType == "" {
		showError(w, req, "rest_create_index: sourceType is required", 400)
		return
	}

	sourceName := req.FormValue("sourceName")
	if sourceName == "" {
		sourceName = indexDef.SourceName
	}
	if sourceName == "" {
		// NOTE: Some sourceTypes (like "nil") don't care if sourceName is "".
		if sourceType == "couchbase" {
			sourceName = indexName // TODO: Revisit default of sourceName as indexName.
		}
	}

	sourceUUID := req.FormValue("sourceUUID") // Defaults to "".
	if sourceUUID == "" {
		sourceUUID = indexDef.SourceUUID
	}

	sourceParams := req.FormValue("sourceParams") // Defaults to "".
	if sourceParams == "" {
		sourceParams = indexDef.SourceParams
	}

	planParams := &PlanParams{}
	planParamsStr := req.FormValue("planParams")
	if planParamsStr != "" {
		err := json.Unmarshal([]byte(planParamsStr), planParams)
		if err != nil {
			showError(w, req, fmt.Sprintf("rest_create_index:"+
				" error parsing planParams: %s, err: %v",
				planParamsStr, err), 400)
			return
		}
	} else {
		planParams = &indexDef.PlanParams
	}

	prevIndexUUID := req.FormValue("prevIndexUUID") // Defaults to "".

	err = h.mgr.CreateIndex(sourceType, sourceName, sourceUUID, sourceParams,
		indexType, indexName, string(indexParams), *planParams, prevIndexUUID)
	if err != nil {
		showError(w, req, fmt.Sprintf("rest_create_index:"+
			" error creating index: %s, err: %v",
			indexName, err), 400)
		return
	}

	mustEncode(w, struct { // TODO: Should return created instead of 200 HTTP code?
		Status string `json:"status"`
	}{Status: "ok"})
}
