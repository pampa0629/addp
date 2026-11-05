"""Owner-local bilingual messages for the synchronous query protocol."""

MESSAGES = {
    'query_authentication_required': ('查询需要有效的服务凭据。', 'A valid service credential is required.'),
    'query_permission_denied': ('当前调用方无权执行此查询。', 'The caller is not allowed to execute this query.'),
    'query_authentication_unavailable': ('查询认证服务暂不可用。', 'Query authentication is unavailable.'),
    'query_invalid': ('查询请求无效或包含不支持的选项。', 'The query request is invalid or contains unsupported options.'),
    'query_source_not_supported': ('查询源类型或连接配置不受支持。', 'The query source type or connection configuration is not supported.'),
    'query_source_not_authorized': ('查询引用了未授权的数据源。', 'The query references an unauthorized source.'),
    'query_not_read_only': ('仅支持已验证的只读查询语法。', 'Only validated read-only query syntax is supported.'),
    'query_function_not_supported': ('查询包含不支持的函数。', 'The query contains an unsupported function.'),
    'query_complexity_exceeded': ('查询超过语法复杂度限制。', 'The query exceeds the syntax complexity limit.'),
    'query_timeout': ('查询已超时，未返回结果。', 'The query timed out without returning results.'),
    'query_runtime_unavailable': ('查询运行时暂不可用。', 'The query runtime is unavailable.'),
    'query_runtime_busy': ('查询运行时繁忙，请稍后重试。', 'The query runtime is busy. Try again later.'),
    'query_driver_failed': ('查询执行失败，未返回结果。', 'Query execution failed without returning results.'),
    'query_column_not_found': ('查询引用的字段不存在。', 'A referenced column could not be resolved.'),
    'query_result_invalid': ('查询结果无效，未返回数据。', 'The query result is invalid. No data was returned.'),
}


def message(code, language_header):
    english = False
    for part in language_header.split(','):
        tag = part.split(';', 1)[0].strip().lower()
        if tag.startswith('zh'):
            break
        if tag.startswith('en'):
            english = True
            break
    return MESSAGES[code][int(english)]
