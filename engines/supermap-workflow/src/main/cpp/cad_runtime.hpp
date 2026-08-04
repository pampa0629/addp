#pragma once

#include "workflow.hpp"

namespace addp::supermap {

addp::workflow::Json inspect_cad(const addp::workflow::Json& params);
addp::workflow::Json render_cad_preview(const addp::workflow::Json& params);

}  // namespace addp::supermap
