# VTON Integration Notes

## Google Vertex AI Virtual Try-On API

### Endpoint
POST https://{REGION}-aiplatform.googleapis.com/v1/projects/{PROJECT_ID}/locations/{REGION}/publishers/google/models/virtual-try-on-001:predict

### Auth
- OAuth2 Bearer token via Application Default Credentials (ADC)
- Scope: https://www.googleapis.com/auth/cloud-platform
- Local dev: `gcloud auth application-default login`
- Production: service account JSON → written to temp file → GOOGLE_APPLICATION_CREDENTIALS
- getAccessToken() in google_vertex.go calls google.FindDefaultCredentials() on EVERY request — no token caching (gap to address)

### Request Shape
```json
{
  "instances": [{
    "personImage": {"image": {"bytesBase64Encoded": "..."}},
    "productImages": [{"image": {"bytesBase64Encoded": "..."}}]
  }],
  "parameters": {
    "sampleCount": 1,
    "baseSteps": 20,
    "personGeneration": "allow_adult",
    "outputOptions": {"mimeType": "image/jpeg", "compressionQuality": 85}
  }
}
```

### Response Shape
```json
{
  "predictions": [{"bytesBase64Encoded": "...", "mimeType": "image/jpeg"}]
}
```

Note: implementation parses only bytesBase64Encoded from predictions[0]. The mimeType field in the response is not captured — hardcoded to "image/jpeg" in VTONResult.

### Parameters Reference
- sampleCount: 1-4 (we use 1)
- baseSteps: affects quality/speed tradeoff (default 32, we use 20)
- personGeneration: "dont_allow" | "allow_adult" | "allow_all"
- safetySetting: "block_medium_and_above" (default)
- addWatermark: bool (default true)
- seed: uint32 (cannot use with watermark=true)
- storageUri: GCS path for direct output (not used — we use base64)
- outputOptions: mimeType (image/jpeg or image/png), compressionQuality (0-100)

### Quotas / Rate Limits
- Quota metric: aiplatform.googleapis.com/online_prediction_requests_per_base_model
- ~50 requests/min per region (approximate, not officially documented)
- 429 = Resource Exhausted → quota exceeded, submit quota increase request
- Max input image size: 10MB
- Max output images per request: 4

### Known Issues / Gaps in Implementation
1. Token not cached — FindDefaultCredentials called on every Generate() call. Should cache token with expiry check.
2. VTON timeout is 30s but Vertex AI inference can take 15-45s. May need to raise to 60s.
3. category field in VTONRequest is computed but NOT sent to the API (API doesn't have a category param — the model infers garment type from the image).
4. GarmentDesc is also not sent (API doesn't accept text descriptions, only images).
5. Error responses from Vertex AI are returned as raw string — no structured error parsing.
6. The response mimeType is hardcoded to "image/jpeg" regardless of what the API returns.

## FASHN Provider

Stub implementation only. `Generate()` returns error "FASHN provider not yet implemented".
API key and mode are configured but never used.

## Look Generation Pipeline

LookComposer (LLM) → ComposeLooks() → []session.Look (stored in session)
LookGenerator (background goroutine) → GenerateAll() → per-look VTON generation
- Ordered: upper_body first, then lower_body (for chaining)
- Chaining: output of upper_body VTON used as person image input for lower_body VTON
- Results polled via GET /session/{id}/looks
- Status: pending → generating → ready | failed

## Category Mapper

MapActionToCategory() in category_mapper.go — keyword matching on Spanish and English terms.
Only affects local routing logic; NOT sent to the Vertex AI API.

## CLI Test Tool

cmd/tryontest/main.go — standalone binary to test VTON without full server.
Usage: go run ./cmd/tryontest -person photo.jpg -garment garment.jpg
Requires: GCP_PROJECT_ID env var + gcloud auth application-default login
Binary already compiled at /tryontest (arm64).
