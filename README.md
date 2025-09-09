# Search Collector AI

**Vibe coding experiment.** This is an attempt to rewrite the stolostron/search-collector project using only AI promts.



### Status Endpoints Available:

  - GET /health - Health check
  - GET /status - Overall system status
  - GET /reconciler/stats - Detailed reconciler statistics
  - GET /reconciler/resources - List managed resources
  - GET /reconciler/resources?type=pods - Filter by resource type
  - GET /reconciler/resources?changed=true - Only changed resources