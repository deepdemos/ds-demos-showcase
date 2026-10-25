import RingCheckbox, {
  type CheckboxProps,
} from '@jetbrains/ring-ui-built/components/checkbox/checkbox';

export type { CheckboxProps };

export function Checkbox(props: CheckboxProps) {
  return <RingCheckbox {...props} />;
}
