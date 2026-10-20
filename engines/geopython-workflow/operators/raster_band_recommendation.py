"""Explainable, full-domain OIF recommendations for three data bands."""

import itertools
import math

import numpy as np
from osgeo import gdal

from .base import OperatorCategory, OperatorMetadata, OperatorParam, OperatorType, OutputPort, register_operator
from .raster_compute import _band, _blocks, _open


def _relative_values(values, anchor, scale):
    """Preserve small differences, dividing before subtraction only on overflow."""
    with np.errstate(over='ignore', invalid='ignore', under='ignore'):
        differences = values - anchor
        overflow = ~np.isfinite(differences)
        differences /= scale
        if overflow.any():
            differences[overflow] = values[overflow] / scale - anchor / scale
    return differences


def _finite(value, name):
    value = float(value)
    if not math.isfinite(value):
        raise ValueError(f'OIF {name} is not representable as a finite Float64 value')
    return value


def _score(standard_deviations, correlation_sum):
    # Scale the sum and use exponents so neither sum(stddev) nor an intermediate
    # division overflows when the final score is representable.
    scale = max(standard_deviations)
    numerator = math.fsum(value / scale for value in standard_deviations)
    mantissa, exponent = math.frexp(scale)
    divisor, divisor_exponent = math.frexp(correlation_sum)
    try:
        return _finite(math.ldexp(mantissa * numerator / divisor, exponent - divisor_exponent), 'score')
    except OverflowError as exc:
        raise ValueError('OIF score is not representable as a finite Float64 value') from exc


def raster_recommend_band_combination(input_raster, bands):
    if not isinstance(bands, list) or not 3 <= len(bands) <= 16:
        raise ValueError('OIF bands must be a list of 3 to 16 unique data band indices')
    if any(isinstance(index, bool) or not isinstance(index, int) for index in bands):
        raise ValueError('OIF bands must contain integer one-based data band indices')
    if len(set(bands)) != len(bands):
        raise ValueError('OIF bands must not contain duplicate indices')
    bands = sorted(bands)
    dataset = _open(input_raster)
    for index in bands:
        if _band(dataset, index).GetColorInterpretation() == gdal.GCI_AlphaBand:
            raise ValueError('OIF candidate bands must not include alpha')
    for index in range(1, dataset.RasterCount + 1):
        candidate = dataset.GetRasterBand(index)
        if candidate.GetColorInterpretation() == gdal.GCI_AlphaBand and gdal.DataTypeIsComplex(candidate.DataType):
            raise ValueError('OIF complex alpha is not supported')

    size = len(bands)
    count = 0
    minima = np.full(size, np.inf)
    maxima = np.full(size, -np.inf)
    anchors = np.zeros(size)
    # The same intersection is used in every pass, including constant candidates.
    for _, _, arrays, masks in _blocks(dataset, bands):
        common = np.logical_and.reduce(masks)
        n = int(common.sum())
        if not n:
            continue
        for index, array in enumerate(arrays):
            values = array[common]
            if not count:
                anchors[index] = values[0]
            minima[index] = min(minima[index], float(values.min()))
            maxima[index] = max(maxima[index], float(values.max()))
        count += n
    if count < 2:
        raise ValueError('OIF requires at least two common valid pixels')
    constants = minima == maxima
    if int((~constants).sum()) < 3:
        raise ValueError('OIF has no scorable three-band combination: fewer than three nonconstant bands')
    scales = np.maximum(np.abs(minima), np.abs(maxima))
    scales[scales == 0] = 1.0

    # Compensated accumulation of block-weighted centered means avoids a large
    # raw-value sum. A separate covariance pass avoids subtracting E[x]^2.
    means = np.zeros(size)
    corrections = np.zeros(size)
    for _, _, arrays, masks in _blocks(dataset, bands):
        common = np.logical_and.reduce(masks)
        n = int(common.sum())
        if not n:
            continue
        for index, array in enumerate(arrays):
            relative = _relative_values(array[common], anchors[index], scales[index])
            term = float(relative.mean()) * (n / count)
            adjusted = term - corrections[index]
            updated = means[index] + adjusted
            corrections[index] = (updated - means[index]) - adjusted
            means[index] = updated

    covariance = np.zeros((size, size))
    corrections = np.zeros_like(covariance)
    for _, _, arrays, masks in _blocks(dataset, bands):
        common = np.logical_and.reduce(masks)
        n = int(common.sum())
        if not n:
            continue
        centered = np.empty((size, n))
        for index, array in enumerate(arrays):
            centered[index] = _relative_values(array[common], anchors[index], scales[index]) - means[index]
        term = (centered @ centered.T) / count
        adjusted = term - corrections
        updated = covariance + adjusted
        corrections = (updated - covariance) - adjusted
        covariance = updated

    normalized_stddev = np.sqrt(np.diag(covariance))
    statistics = []
    for index, band in enumerate(bands):
        with np.errstate(over='ignore', invalid='ignore', under='ignore'):
            shift = means[index] * scales[index]
            mean = anchors[index] + shift
            if not np.isfinite(mean):
                mean = (anchors[index] / scales[index] + means[index]) * scales[index]
            stddev = normalized_stddev[index] * scales[index]
        stddev = _finite(stddev, 'standard deviation')
        if not constants[index] and stddev == 0:
            raise ValueError('OIF nonzero standard deviation is not representable as Float64')
        statistics.append({'band': band, 'mean': _finite(mean, 'mean'), 'stddev': stddev,
                           'score_status': 'zero_variance' if constants[index] else 'eligible'})

    correlations = [[None] * size for _ in bands]
    for i in range(size):
        if constants[i]:
            continue
        correlations[i][i] = 1.0
        for j in range(i + 1, size):
            if constants[j]:
                continue
            correlation = _finite((covariance[i, j] / normalized_stddev[i]) / normalized_stddev[j], 'correlation')
            # Roundoff in dot/sqrt may put a perfectly correlated pair a few
            # ulps outside [-1, 1]; larger discrepancies are calculation errors.
            if abs(correlation) > 1 + 32 * np.finfo(float).eps:
                raise ValueError('OIF correlation lies outside its mathematical range')
            correlation = min(1.0, max(-1.0, correlation))
            correlations[i][j] = correlations[j][i] = correlation

    rankings, excluded = [], []
    for indices in itertools.combinations(range(size), 3):
        combination = [bands[index] for index in indices]
        if any(constants[index] for index in indices):
            excluded.append({'bands': combination, 'reason': 'zero_variance'})
            continue
        denominator = math.fsum(abs(correlations[i][j]) for i, j in itertools.combinations(indices, 2))
        status = 'unbounded' if denominator == 0 else 'finite'
        score = None if denominator == 0 else _score([statistics[index]['stddev'] for index in indices], denominator)
        rankings.append({'bands': combination, 'score_status': status, 'score': score})
    rankings.sort(key=lambda row: (row['score_status'] != 'unbounded', -(row['score'] or 0), row['bands']))
    for rank, row in enumerate(rankings, 1):
        row['rank'] = rank
    return {'method': 'oif', 'strategy': 'full', 'value_domain': 'stored', 'candidate_bands': bands,
            'common_valid_count': count, 'common_invalid_count': dataset.RasterXSize * dataset.RasterYSize - count,
            'band_statistics': statistics, 'correlation_matrix': correlations, 'rankings': rankings,
            'excluded_combinations': excluded, 'recommended_bands': rankings[0]['bands'].copy()}


_METADATA = OperatorMetadata(
    name='raster_recommend_band_combination', type=OperatorType.SPATIAL,
    category=OperatorCategory.RASTER_STATISTICS, description='栅格三波段组合推荐（OIF）',
    brief_description='按共同有效像元的总体标准差与相关性推荐三波段集合，不分配 RGB 通道',
    overview='全量分块计算全部候选共同有效像元的 OIF 评分，返回可解释排名和推荐集合，不代表专题识别最优。',
    params=[OperatorParam(name='input_raster', data_type='raster', type='input', description='当前工作流的栅格输入'),
            OperatorParam(name='bands', data_type='list[int]', type='param', description='显式选择 3–16 个互不重复的一基数据波段，不能包含 alpha')],
    output_ports=[OutputPort(name='default', type='object', description='共同域统计、相关矩阵、OIF 排名及推荐三波段集合')],
    execution_modes=['workflow'], effects=['read'], attributes={'resource_groups': ['raster']},
    use_cases=['多光谱三波段组合统计建议', '显式选波段前比较信息差异与冗余'],
    notes=['使用全部候选共同有效像元和原始存储值，不隐式应用 scale/offset',
           '零方差候选排除评分；无界评分以 null 和 unbounded 表达，不输出 Infinity',
           '推荐集合不指定 RGB 通道，不生成影像，不保证专题识别效果'],
    workflow_example={'operator': 'raster_recommend_band_combination',
                      'params': {'input_raster': {'$ref': 'source'}, 'bands': [1, 2, 3]}})

OPERATORS = dict([register_operator(_METADATA, raster_recommend_band_combination)])
