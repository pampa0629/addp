from dataclasses import dataclass
import re
from urllib.parse import parse_qs, quote, unquote, urlsplit


@dataclass(frozen=True)
class Ref:
    area: str
    key: str
    kind: str = "object"

    @classmethod
    def parse(cls, value: str):
        if not isinstance(value, str) or not value.startswith("addp-infra://object/") or any(ord(c) < 32 for c in value):
            raise ValueError("invalid object reference")
        parsed = urlsplit(value)
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        if parsed.scheme != "addp-infra" or parsed.netloc != "object" or parsed.fragment or query not in (
            {"type": ["object"]}, {"type": ["prefix"]}
        ):
            raise ValueError("invalid platform object reference")
        encoded = parsed.path.removeprefix("/").split("/")
        if any(re.search(r"%(?![0-9a-fA-F]{2})", part) for part in encoded):
            raise ValueError("invalid object path escape")
        parts = [unquote(part, errors="strict") for part in encoded]
        if len(parts) < 2 or parts[0] not in {"standard", "manager", "develop"} or any(
            part in {"", ".", ".."} or any(c in part for c in "/\\\0") for part in parts
        ):
            raise ValueError("invalid platform object path")
        return cls(parts[0], "/".join(parts[1:]), query["type"][0])

    def uri(self):
        value = f"addp-infra://object/{self.area}/" + "/".join(quote(p, safe="") for p in self.key.split("/")) + f"?type={self.kind}"
        self.parse(value)
        return value
