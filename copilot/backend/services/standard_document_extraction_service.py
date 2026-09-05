"""Standard 文档候选提炼应用服务；不持久化、不发布标准。"""

from chains.standard_document_extraction_chain import StandardDocumentExtractionChain
from models.standard_document_models import (
    StandardDocumentExtractRequest,
    StandardDocumentExtractResponse,
)


class StandardDocumentExtractionService:
    def __init__(self, llm, chain=None):
        self.chain = chain or StandardDocumentExtractionChain(llm)

    async def run(
        self, request: StandardDocumentExtractRequest
    ) -> StandardDocumentExtractResponse:
        result = await self.chain.extract(request)
        intervals: dict[str, list[tuple[int, int]]] = {}
        for section in request.sections:
            intervals.setdefault(section.section_path, []).append(
                (section.start_line, section.end_line)
            )
        candidates = []
        for candidate in result.candidates:
            valid_evidences = []
            for evidence in candidate.evidences:
                path_intervals = intervals.get(evidence.section_path)
                if path_intervals is None:
                    continue
                if any(
                    start <= evidence.start_line <= evidence.end_line <= end
                    for start, end in path_intervals
                ):
                    valid_evidences.append(evidence)
            if valid_evidences:
                candidates.append(candidate.model_copy(update={"evidences": valid_evidences}))
        return StandardDocumentExtractResponse(candidates=candidates)
