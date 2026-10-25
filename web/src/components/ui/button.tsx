import RingButton, { type ButtonProps } from '@jetbrains/ring-ui-built/components/button/button';

export type { ButtonProps };

export function Button(props: ButtonProps) {
  return <RingButton {...props} />;
}
