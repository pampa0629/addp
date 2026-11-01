"""Fail-closed Spark 3.5.0 Catalyst boundary, before SQL can run a command.

Only an owner-created, extension-free query session may call this boundary.
It neither resolves tables nor executes SQL. Catalog names come from validated
execution grants, never from the query's suggested source connections.
"""

from __future__ import annotations

from dataclasses import dataclass

MAX_QUERY_BYTES = 256 * 1024

class QueryValidationError(Exception):
    """Public diagnostic contains no SQL, physical path or JDBC credentials."""


_LOGICAL = 'org.apache.spark.sql.catalyst.plans.logical.'
_ANALYSIS = 'org.apache.spark.sql.catalyst.analysis.'
_EXPRESSION = 'org.apache.spark.sql.catalyst.expressions.'

_PLANS = frozenset(_LOGICAL + name for name in (
    'Project', 'Filter', 'Aggregate', 'Sort', 'GlobalLimit', 'LocalLimit',
    'Join', 'SubqueryAlias', 'Distinct', 'Union', 'Intersect', 'Except',
    'OneRowRelation', 'UnresolvedWith',
)) | {_ANALYSIS + 'UnresolvedRelation', _ANALYSIS + 'UnresolvedHaving'}

_EXPRESSIONS = frozenset(_EXPRESSION + name for name in (
    'Alias', 'Literal', 'Cast', 'Add', 'Subtract', 'Multiply', 'Divide',
    'IntegralDivide', 'Remainder', 'UnaryMinus', 'UnaryPositive',
    'EqualTo', 'EqualNullSafe', 'GreaterThan', 'GreaterThanOrEqual',
    'LessThan', 'LessThanOrEqual', 'And', 'Or', 'Not', 'IsNull', 'IsNotNull',
    'In', 'InSubquery', 'ListQuery', 'ScalarSubquery', 'Exists',
    'Like', 'RLike', 'CaseWhen', 'If', 'SortOrder',
)) | frozenset(_ANALYSIS + name for name in (
    'UnresolvedAttribute', 'UnresolvedAlias', 'UnresolvedStar',
    'UnresolvedFunction', 'UnresolvedExtractValue', 'PosParameter',
))

# Deliberately bounded pure built-ins. In particular, no reflect/java_method,
# file/URL access, session settings, external UDF, generator or TVF is admitted.
_FUNCTIONS = frozenset((
    'abs', 'ceil', 'ceiling', 'floor', 'round', 'bround', 'sqrt', 'pow', 'power',
    'exp', 'ln', 'log', 'log10', 'sign', 'signum', 'greatest', 'least',
    'count', 'sum', 'avg', 'min', 'max', 'stddev', 'stddev_pop', 'stddev_samp',
    'variance', 'var_pop', 'var_samp',
    'coalesce', 'ifnull', 'nvl', 'nullif', 'isnull', 'isnotnull', 'isnan',
    'lower', 'upper', 'length', 'char_length', 'character_length',
    'concat', 'concat_ws', 'substring', 'substr', 'trim', 'ltrim', 'rtrim',
    'replace', 'instr', 'locate', 'startswith', 'endswith', 'contains',
    'year', 'quarter', 'month', 'day', 'dayofmonth', 'dayofweek', 'dayofyear',
    'hour', 'minute', 'second', 'date_add', 'date_sub', 'datediff',
    'date_format', 'date_trunc', 'trunc', 'to_date', 'to_timestamp',
    'current_date', 'current_timestamp',
))

_SUBQUERIES = frozenset(_EXPRESSION + name for name in ('ScalarSubquery', 'ListQuery', 'Exists'))


@dataclass(frozen=True)
class ValidatedQuery:
    sql: str
    source_catalogs: frozenset[str]
    source_relations: frozenset[tuple[str, str, str]]


def _sequence(value):
    iterator = value.iterator()
    while iterator.hasNext():
        yield iterator.next()


def validate_query(spark, query, catalogs):
    """Validate the entire native unresolved tree without invoking spark.sql.

    Unknown Catalyst classes or unsupported syntax fail closed, including when
    nested in CTE definitions and scalar/EXISTS/IN subqueries. This is not a
    parser based on SELECT prefixes, regular expressions or diagnostic text.
    """
    if (spark.version != '3.5.0' or not isinstance(query, str) or not query.strip()
            or len(query.encode('utf-8')) > MAX_QUERY_BYTES
            or not isinstance(catalogs, (set, frozenset)) or not catalogs
            or any(not isinstance(name, str) or not name or name != name.strip()
                   or '.' in name for name in catalogs)):
        raise QueryValidationError('query_invalid')
    try:
        root = spark._jsparkSession.sessionState().sqlParser().parsePlan(query)
        pending = [('plan', root, frozenset(), 0)]
        used = set()
        relations = set()
        remaining = 10000
        while pending:
            kind, node, ctes, depth = pending.pop()
            remaining -= 1
            if remaining < 0 or depth > 128:
                raise QueryValidationError('query_complexity_exceeded')
            class_name = node.getClass().getName()
            allowed = _PLANS if kind == 'plan' else _EXPRESSIONS
            if class_name not in allowed:
                raise QueryValidationError('query_not_read_only')
            if class_name == _LOGICAL + 'UnresolvedWith':
                # Spark 3.5 CTEs are non-recursive. A definition can reference
                # earlier definitions, but not itself or a later definition.
                visible, names = set(ctes), set()
                for pair in _sequence(node.cteRelations()):
                    name = pair._1()
                    if name in names:
                        raise QueryValidationError('query_invalid')
                    pending.append(('plan', pair._2(), frozenset(visible), depth + 1))
                    visible.add(name)
                    names.add(name)
                pending.append(('plan', node.child(), frozenset(visible), depth + 1))
                continue
            if class_name == _ANALYSIS + 'UnresolvedRelation':
                parts = tuple(_sequence(node.multipartIdentifier()))
                if node.isStreaming() or not node.options().isEmpty():
                    raise QueryValidationError('query_not_read_only')
                if len(parts) == 1 and parts[0] in ctes:
                    pass
                elif len(parts) == 3 and parts[0] in catalogs and all(parts):
                    used.add(parts[0])
                    relations.add(parts)
                else:
                    raise QueryValidationError('query_source_not_authorized')
            if class_name == _ANALYSIS + 'UnresolvedFunction':
                name = tuple(_sequence(node.nameParts()))
                if len(name) != 1 or name[0].lower() not in _FUNCTIONS:
                    raise QueryValidationError('query_function_not_supported')
            if class_name in _SUBQUERIES:
                pending.append(('plan', node.plan(), ctes, depth + 1))
            if kind == 'plan':
                pending.extend(('expression', expression, ctes, depth + 1)
                               for expression in _sequence(node.expressions()))
            pending.extend((kind, child, ctes, depth + 1)
                           for child in _sequence(node.children()))
        return ValidatedQuery(query, frozenset(used), frozenset(relations))
    except QueryValidationError:
        raise
    except Exception:
        # Native parse/bridge errors can contain SQL or physical names.
        raise QueryValidationError('query_invalid') from None
