/**
 * 密码相关的口径，注册页与找回密码页共用一份。
 *
 * 前端校验只为省一次往返，真正的判定在服务端；所以这里的值与后端保持一致，
 * 改的时候两边一起改。**两页各写一个 `10` 的问题是**：改了一处、另一处悄悄
 * 放行一个服务端会拒的口令，用户看到的是「明明填了，提交才说太短」。
 */
export const PASSWORD_MIN = 10;

/**
 * 只在非空时判定：空输入交给表单的 `required`，这里不抢话说。
 */
export function passwordLengthError(password: string): string | undefined {
  return password.length > 0 && password.length < PASSWORD_MIN
    ? `密码至少 ${PASSWORD_MIN} 位`
    : undefined;
}
