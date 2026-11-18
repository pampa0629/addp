#include <libaec.h>
#include <stdint.h>
#include <stdlib.h>

/* Check the admitted ECMWF packing profile inside the isolated GRIB worker.
 * The decoded scratch buffer is private to this function. */
int addp_grib_ccsds_complete(const uint8_t *payload, size_t length,
                           unsigned int points, unsigned int bits)
{
    const unsigned int block_size = 32;
    if (!payload || !length || !points || points > (1U << 24) ||
        !bits || bits > 16)
        return 0;

    size_t padded_points = ((size_t)points + block_size - 1) / block_size * block_size;
    size_t output_bytes = padded_points * ((bits + 7) / 8);
    uint8_t *scratch = malloc(output_bytes);
    if (!scratch)
        return 0;

    struct aec_stream stream = {
        .next_in = payload,
        .avail_in = 0,
        .next_out = scratch,
        .avail_out = output_bytes,
        .bits_per_sample = bits,
        .block_size = block_size,
        .rsi = 128,
        .flags = AEC_DATA_3BYTE | AEC_DATA_MSB | AEC_DATA_PREPROCESS,
    };
    int result = aec_decode_init(&stream);
    int complete = 0;
    if (result == AEC_OK) {
        /* Bulk input may count look-ahead bytes as consumed. Stop supplying
         * bytes at the first complete output so that trailing bytes stay out. */
        for (size_t supplied = 0; supplied < length && stream.avail_out; ++supplied) {
            stream.avail_in = 1;
            result = aec_decode(&stream, AEC_NO_FLUSH);
            if (result != AEC_OK || stream.avail_in)
                break;
        }
        complete = result == AEC_OK && stream.total_out == output_bytes &&
                   stream.total_in == length;
        aec_decode_end(&stream);
    }
    free(scratch);
    return complete;
}
