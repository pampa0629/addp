package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapS3MConversionService.*;
import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.supermap.sps.core.parameters.impls.SingleOutputImpl;
import java.io.IOException;

final class SuperMapConversionProcesses {
  public static final class OSGBSceneToS3MProcess extends SuperMapBaseProcess {
    private final SingleOutputImpl<ObjectNode> output;

    OSGBSceneToS3MProcess(JsonNode params) {
      super("osgb_scene_to_s3m");
      addStringInput("params_json", params.toString());
      this.output = addObjectOutput("s3m", ObjectNode.class);
    }

    @Override
    public boolean execute() {
      try {
        JsonNode params =
            MAPPER.readTree(String.valueOf(parameters.getInput("params_json").getValue()));
        output.setValue(convertOSGBSceneToS3M(params));
        return true;
      } catch (IOException ex) {
        throw new IllegalArgumentException("invalid osgb_scene_to_s3m params", ex);
      }
    }
  }
}
