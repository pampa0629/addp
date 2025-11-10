package service

// CodeGenerator 抽象出所有代码生成模型的统一接口
type CodeGenerator interface {
	GenerateCode(prompt string) (string, error)
	Provider() string
}
