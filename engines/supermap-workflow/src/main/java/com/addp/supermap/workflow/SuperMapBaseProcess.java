package com.addp.supermap.workflow;

import static com.addp.supermap.workflow.SuperMapWorkflowRuntime.*;

import com.supermap.sps.core.parameters.ISingleDataDefinition;
import com.supermap.sps.core.parameters.impls.ConstantValueProvider;
import com.supermap.sps.core.parameters.impls.DefaultSingleDataDefinition;
import com.supermap.sps.core.parameters.impls.ParametersImpl;
import com.supermap.sps.core.parameters.impls.SingleInputImpl;
import com.supermap.sps.core.parameters.impls.SingleOutputImpl;
import com.supermap.sps.core.workflow.impls.AbstractProcess;

abstract class SuperMapBaseProcess extends AbstractProcess {
  SuperMapBaseProcess(String name) {
    super("addp.supermap.workflow", name);
    this.parameters = new ParametersImpl(this);
  }

  @Override
  public String getFactory() {
    return SPS_FACTORY;
  }

  protected void addStringInput(String name, String value) {
    ISingleDataDefinition<String> def = new DefaultSingleDataDefinition<>(String.class);
    SingleInputImpl<String> input = new SingleInputImpl<>(name, def);
    input.setValueProvider(new ConstantValueProvider<>(def, value));
    this.parameters.addInput(input);
  }

  protected void addBooleanInput(String name, boolean value) {
    ISingleDataDefinition<Boolean> def = new DefaultSingleDataDefinition<>(Boolean.class);
    SingleInputImpl<Boolean> input = new SingleInputImpl<>(name, def);
    input.setValueProvider(new ConstantValueProvider<>(def, value));
    this.parameters.addInput(input);
  }

  protected void addDoubleInput(String name, double value) {
    ISingleDataDefinition<Double> def = new DefaultSingleDataDefinition<>(Double.class);
    SingleInputImpl<Double> input = new SingleInputImpl<>(name, def);
    input.setValueProvider(new ConstantValueProvider<>(def, value));
    this.parameters.addInput(input);
  }

  protected void addIntegerInput(String name, int value) {
    ISingleDataDefinition<Integer> def = new DefaultSingleDataDefinition<>(Integer.class);
    SingleInputImpl<Integer> input = new SingleInputImpl<>(name, def);
    input.setValueProvider(new ConstantValueProvider<>(def, value));
    this.parameters.addInput(input);
  }

  protected void addStringArrayInput(String name, String[] value) {
    ISingleDataDefinition<String[]> def = new DefaultSingleDataDefinition<>(String[].class);
    SingleInputImpl<String[]> input = new SingleInputImpl<>(name, def);
    input.setValueProvider(new ConstantValueProvider<>(def, value));
    this.parameters.addInput(input);
  }

  protected <T> void addObjectInput(String name, Class<T> type) {
    ISingleDataDefinition<T> def = new DefaultSingleDataDefinition<>(type);
    SingleInputImpl<T> input = new SingleInputImpl<>(name, def);
    input.setRequired(true);
    this.parameters.addInput(input);
  }

  protected <T> SingleOutputImpl<T> addObjectOutput(String name, Class<T> type) {
    ISingleDataDefinition<T> def = new DefaultSingleDataDefinition<>(type);
    SingleOutputImpl<T> output = new SingleOutputImpl<>(name, def);
    this.parameters.addOutput(output);
    return output;
  }
}
