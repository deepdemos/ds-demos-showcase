import RingInput, {
  Size,
  type InputProps as RingInputProps,
} from '@jetbrains/ring-ui-built/components/input/input';

export type InputProps = RingInputProps & {
  /** 让输入框填满父容器；表单里默认都该是 true。 */
  fullWidth?: boolean;
};

export function Input({ fullWidth, ...props }: InputProps) {
  const explicitSize = (props as { size?: Size }).size;
  // 显式 size 优先：fullWidth 只是“没指定宽度时填满父容器”的默认值。
  return (
    <RingInput
      {...props}
      {...(fullWidth && explicitSize === undefined ? { size: Size.FULL } : {})}
    />
  );
}
