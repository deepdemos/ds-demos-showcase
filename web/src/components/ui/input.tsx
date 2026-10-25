import RingInput, { type InputProps } from '@jetbrains/ring-ui-built/components/input/input';

export type { InputProps };

export function Input(props: InputProps) {
  return <RingInput {...props} />;
}
