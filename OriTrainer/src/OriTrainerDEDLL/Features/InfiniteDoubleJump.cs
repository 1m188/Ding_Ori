using System;
using System.Reflection;
using System.Threading;

namespace OriTrainerDEDLL.Features
{
    // 无限二段跳。
    //
    // 游戏的门槛在 SeinDoubleJump.CanDoubleJump，共 5 项：
    //     enabled && !IsOnGround && m_numberOfJumpsAvailable != 0
    //             && m_remainingLockTime <= 0 && !IsInside(限制区)
    //
    // 前三项之外，这里覆盖：
    //   ① 授予基础二段跳能力 —— 没有该能力时组件不会被激活，跳跃分支根本不进入；
    //   ② 持续维持 m_numberOfJumpsAvailable —— 落地时游戏会 ResetDoubleJump() 归位，
    //      空中每跳一次也会自减，所以必须持续写；
    //   ③ 持续清 m_remainingLockTime —— 游戏会 LockForDuration() 临时上锁，
    //      只写次数仍会被锁挡住（golang 版漏了这一项）。
    //
    // 不做官方 CheatsHandler.InfiniteDoubleJumps 那条路：它和成就闸门 DebugWasEnabled
    // 同属一套作弊系统，本功能只碰能力与跳跃字段，不进入该系统。
    public static class InfiniteDoubleJump
    {
        private const int IntervalMs = 10;
        private const int JumpsAvailable = 999;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fJumps;    // m_numberOfJumpsAvailable
        private static FieldInfo _fLockTime; // m_remainingLockTime

        private static Timer _timer;

        public static void Start()
        {
            if (_timer != null) return; // 幂等：重复 Start 不重复起定时器

            _fJumps = typeof(SeinDoubleJump).GetField("m_numberOfJumpsAvailable", Private);
            _fLockTime = typeof(SeinDoubleJump).GetField("m_remainingLockTime", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每 10ms 静默空转
            if (_fJumps == null || _fLockTime == null)
                throw new Exception("SeinDoubleJump 的字段名与预期不符，功能无法工作");

            _timer = new Timer(Tick, null, 0, IntervalMs);
        }

        public static void Stop()
        {
            if (_timer == null) return;

            _timer.Dispose();
            _timer = null;

            // 刻意不还原 HasAbility：游戏的 SeinNestedPrefab.IsInstantiated setter
            // 置 false 时会 Destroy() 掉能力组件，还原后再次开启无法只靠写标志位重建，
            // 会变成"关了再开就失效"。副作用是关闭后仍保留普通二段跳（一次空中跳）。
        }

        private static void Tick(object state)
        {
            // 定时器回调里的未捕获异常会终止整个进程（即游戏），必须自己兜住
            try
            {
                // 主菜单/读档过程中 Sein 为 null，显式判空
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                // ① 能力开关（CharacterAbility.HasAbility 是普通类字段，直接写）
                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities != null && playerAbilities.DoubleJump != null)
                    playerAbilities.DoubleJump.HasAbility = true;

                // ②③ 跳跃次数与锁定时间（两个都是 private，只能反射）
                SeinAbilities abilities = sein.Abilities;
                if (abilities == null) return;

                SeinDoubleJump jump = abilities.DoubleJump;
                if (jump == null) return;

                _fJumps.SetValue(jump, JumpsAvailable);
                _fLockTime.SetValue(jump, 0f);
            }
            catch { }
        }
    }
}
