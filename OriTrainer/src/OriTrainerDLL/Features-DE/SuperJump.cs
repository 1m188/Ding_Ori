using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 超级跳：把跳跃高度放大到原值的 Multiplier 倍。
    //
    // ---- 原理：hook 高度→速度换算，而不是每帧写高度字段 ----
    // SeinJump 的跳跃路径（后空翻 / 1·2·3 段跑跳 / 1·2·3 段站立跳 / 墙跳 / 蹲跳）
    // 全部汇聚到同一个入口 SeinJump.CalculateSpeedFromHeight(height)：
    //   return PhysicsHelper.CalculateSpeedFromHeight(height, this.Sein...GravityStrength);
    // 起跳时把 height 放大成 height*Multiplier，速度即放大为 原速*Sqrt(Multiplier)。
    // 单点覆盖全部跳跃，且只放大每次起跳的初始竖直速度，不动水平速度/重力/下落/冲量。
    //
    // ---- 为什么 hook 实例方法而不是静态 PhysicsHelper ----
    // 静态 PhysicsHelper.CalculateSpeedFromHeight 还被 JumperEnemy（敌人跳）和
    // SpringSeinAction（弹簧）复用，hook 静态会把敌人和弹簧也放大。hook SeinJump
    // 的实例方法只影响 Ori 本人。replacement 内部仍调那个静态方法（未受影响）。
    //
    // ---- 为什么不用每帧写 5 个高度字段（旧实现） ----
    // 旧实现要在每帧遍历写 First/Second/Third/Crouch/BackflipJumpHeight 5 个字段，
    // 还要捕获原值、识别组件重建后重捕、Stop 时才敢还原。hook 是方法级替换，
    // 与 SeinJump 实例无关：组件重建天然免疫，Stop 无条件 Unhook 还原。
    public static class SuperJump
    {
        private const float Multiplier = 2.5f;

        private static Hooks.Hook _hook;

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            // 目标：SeinJump.CalculateSpeedFromHeight(float)->float，public 实例方法。
            // 属性 getter 用正规取法（GetProperty+GetGetMethod），这里普通方法直接 GetMethod。
            MethodInfo target = typeof(SeinJump).GetMethod("CalculateSpeedFromHeight",
                BindingFlags.Public | BindingFlags.Instance) ?? throw new Exception("SeinJump.CalculateSpeedFromHeight 与预期不符，功能无法工作");

            if (_hook == null)
                _hook = Hooks.Hook.Apply(target,
                    typeof(SuperJump).GetMethod("OnCalculateSpeedFromHeight",
                        BindingFlags.NonPublic | BindingFlags.Static));
        }

        public static void Stop()
        {
            if (_hook == null) return; // 幂等

            _hook.Dispose();
            _hook = null;
        }

        // 由游戏每次起跳时调用（代替 SeinJump.CalculateSpeedFromHeight）。
        // 实例方法被替换后，this 以第一参数形式传来，签名 compatible。
        private static float OnCalculateSpeedFromHeight(SeinJump jump, float height)
        {
            // 兜底：Sein 尚未就绪时按原高度返回，不让游戏崩。
            if (jump == null || jump.Sein == null)
                return height;

            // 直接调静态 PhysicsHelper（未被 hook），不递归目标方法：
            //   Sqrt(2 * g * (height*M)) == Sqrt(2*g*height) * Sqrt(M)
            return PhysicsHelper.CalculateSpeedFromHeight(
                height * Multiplier,
                jump.Sein.PlatformBehaviour.Gravity.BaseSettings.GravityStrength);
        }
    }
}