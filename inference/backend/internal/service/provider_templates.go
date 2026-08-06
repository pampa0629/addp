package service

const (
	ProviderCategoryCloud  = "cloud"
	ProviderCategoryLocal  = "local"
	ProviderCategoryCustom = "custom"

	CredentialModeRequired = "required"
	CredentialModeOptional = "optional"

	ModelDiscoveryOpenAI = "openai_models"
	ModelDiscoveryNone   = "none"
)

type SuggestedModel struct {
	UpstreamModel                string   `json:"upstream_model"`
	Operations                   []string `json:"operations"`
	Modalities                   []string `json:"modalities"`
	Dimension                    int      `json:"dimension,omitempty"`
	ProfileCode                  string   `json:"profile_code"`
	CapabilityPreset             string   `json:"capability_preset"`
	ChatMaxOutputTokensParameter string   `json:"chat_max_output_tokens_parameter,omitempty"`
	ChatTemperatureMode          string   `json:"chat_temperature_mode,omitempty"`
}

type ProviderTemplate struct {
	Code             string           `json:"code"`
	Category         string           `json:"category"`
	AdapterType      string           `json:"adapter_type"`
	DefaultEndpoint  string           `json:"default_endpoint"`
	EndpointEditable bool             `json:"endpoint_editable"`
	CredentialMode   string           `json:"credential_mode"`
	ModelDiscovery   string           `json:"model_discovery"`
	DocumentationURL string           `json:"documentation_url"`
	SuggestedModels  []SuggestedModel `json:"suggested_models"`
}

var providerTemplates = []ProviderTemplate{
	{Code: "openai", Category: ProviderCategoryCloud, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "https://api.openai.com/v1", CredentialMode: CredentialModeRequired, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://platform.openai.com/docs/api-reference"},
	{Code: "dashscope-compatible", Category: ProviderCategoryCloud, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", CredentialMode: CredentialModeRequired, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope"},
	{Code: "dashscope-multimodal", Category: ProviderCategoryCloud, AdapterType: AdapterDashScopeMultimodal, DefaultEndpoint: "https://dashscope.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding", CredentialMode: CredentialModeRequired, ModelDiscovery: ModelDiscoveryNone, DocumentationURL: "https://help.aliyun.com/zh/model-studio/multimodal-embedding-api-reference", SuggestedModels: []SuggestedModel{{UpstreamModel: "qwen3-vl-embedding", Operations: []string{"embedding"}, Modalities: []string{"text", "image"}, Dimension: 2560, ProfileCode: "multimodal-embedding", CapabilityPreset: "multimodal_embedding_2560"}}},
	{Code: "deepseek", Category: ProviderCategoryCloud, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "https://api.deepseek.com/v1", CredentialMode: CredentialModeRequired, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://api-docs.deepseek.com/"},
	{Code: "siliconflow", Category: ProviderCategoryCloud, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "https://api.siliconflow.cn/v1", CredentialMode: CredentialModeRequired, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://docs.siliconflow.cn/"},
	{Code: "openrouter", Category: ProviderCategoryCloud, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "https://openrouter.ai/api/v1", CredentialMode: CredentialModeRequired, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://openrouter.ai/docs/api/reference/overview"},
	{Code: "ollama", Category: ProviderCategoryLocal, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "http://localhost:11434/v1", EndpointEditable: true, CredentialMode: CredentialModeOptional, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://docs.ollama.com/api/openai-compatibility"},
	{Code: "vllm", Category: ProviderCategoryLocal, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "http://localhost:8000/v1", EndpointEditable: true, CredentialMode: CredentialModeOptional, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html"},
	{Code: "lm-studio", Category: ProviderCategoryLocal, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "http://localhost:1234/v1", EndpointEditable: true, CredentialMode: CredentialModeOptional, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://lmstudio.ai/docs/developer/openai-compat"},
	{Code: "xinference", Category: ProviderCategoryLocal, AdapterType: AdapterOpenAICompatible, DefaultEndpoint: "http://localhost:9997/v1", EndpointEditable: true, CredentialMode: CredentialModeOptional, ModelDiscovery: ModelDiscoveryOpenAI, DocumentationURL: "https://inference.readthedocs.io/en/latest/user_guide/client_api.html"},
	{Code: "custom-openai-compatible", Category: ProviderCategoryCustom, AdapterType: AdapterOpenAICompatible, EndpointEditable: true, CredentialMode: CredentialModeOptional, ModelDiscovery: ModelDiscoveryOpenAI},
}

func ListProviderTemplates() []ProviderTemplate {
	result := make([]ProviderTemplate, len(providerTemplates))
	for index, template := range providerTemplates {
		result[index] = template
		result[index].SuggestedModels = make([]SuggestedModel, len(template.SuggestedModels))
		for modelIndex, model := range template.SuggestedModels {
			result[index].SuggestedModels[modelIndex] = model
			result[index].SuggestedModels[modelIndex].Operations = append([]string(nil), model.Operations...)
			result[index].SuggestedModels[modelIndex].Modalities = append([]string(nil), model.Modalities...)
		}
	}
	return result
}
