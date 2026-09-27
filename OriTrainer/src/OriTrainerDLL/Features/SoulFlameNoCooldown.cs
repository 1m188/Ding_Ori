using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 灵魂链接无需冷却：hook 游戏自己的 HandleCooldown()，让冷却永不衰减，
    // 并在 replacement 里顺带把 m_cooldownRemaining 清 0。
    public static class SoulFlameNoCooldown
    {
        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static MethodInfo _target;    // SeinSoulFlame.HandleCooldown
        private static MethodInfo _replacement; // 我们的 replacement
        private static FieldInfo _fCooldown;  // m_cooldownRemaining

        public static void Start()
        {
            if (Hooks.IsHooked(_target)) return; // 幂等：重复 Start 不重复 hook

            Type t = typeof(SeinSoulFlame);
            _target = t.GetMethod("HandleCooldown", Private);
            _fCooldown = t.GetField("m_cooldownRemaining", Private);

            // 字段/方法名对不上就直接失败（Loader 会记进错误日志），而不是静默空转
            if (_target == null || _fCooldown == null)
                throw new Exception("SeinSoulFlame 的 HandleCooldown/m_cooldownRemaining 与预期不符，功能无法工作");

            // replacement 必须是实例方法，签名与 HandleCooldown 兼容（this=SeinSoulFlame）
            _replacement = typeof(SoulFlameNoCooldown).GetMethod("OnHandleCooldown",
                BindingFlags.NonPublic | BindingFlags.Static);

            Hooks.Replace(_target, _replacement);
        }

        public static void Stop()
        {
            if (!Hooks.IsHooked(_target)) return; // 幂等

            Hooks.Unhook(_target);
            _target = null;
            _replacement = null;
            _fCooldown = null;
        }

        // 由游戏主线程每帧调用（代替 HandleCooldown）。
        // 清冷却字段 + 什么都不做（返回即"空操作"）。
        private static void OnHandleCooldown(SeinSoulFlame soulFlame)
        {
            try
            {
                _fCooldown.SetValue(soulFlame, 0f);
            }
            catch { }
        }
    }
}