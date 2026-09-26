using System;
using System.Reflection;

namespace OriTrainerDLL.Features
{
    // 无限二段跳：授予二段跳能力并持续维持跳跃次数与锁定时间。
    // 实现同终极版，必须挂主线程钩子：能力组件要经
    // SeinPrefabFactory.EnsureRightPrefabsAreThereForAbilities() 用 Object.Instantiate
    // 实例化（Unity API，只能在主线程调），所以挂 Game.Events.Scheduler.OnGameFixedUpdate。
    public static class InfiniteDoubleJump
    {
        private const int JumpsAvailable = 999;

        private static readonly BindingFlags Private =
            BindingFlags.NonPublic | BindingFlags.Instance;

        private static FieldInfo _fJumps;    // m_numberOfJumpsAvailable
        private static FieldInfo _fLockTime; // m_remainingLockTime

        private static Action _hook; // 保留引用以便 Stop 时注销

        public static void Start()
        {
            if (_hook != null) return; // 幂等：重复 Start 不重复挂载

            _fJumps = typeof(SeinDoubleJump).GetField("m_numberOfJumpsAvailable", Private);
            _fLockTime = typeof(SeinDoubleJump).GetField("m_remainingLockTime", Private);

            // 字段名对不上就直接失败（Loader 会记进错误日志），而不是每帧静默空转
            if (_fJumps == null || _fLockTime == null)
                throw new Exception("SeinDoubleJump 的字段名与预期不符，功能无法工作");

            GameScheduler scheduler = Game.Events.Scheduler ?? throw new Exception("GameScheduler 尚未就绪（游戏未启动完成），功能无法挂载");

            _hook = OnGameFixedUpdate;
            scheduler.OnGameFixedUpdate.Add(_hook);
        }

        public static void Stop()
        {
            if (_hook == null) return;

            Game.Events.Scheduler.OnGameFixedUpdate.Remove(_hook);
            _hook = null;

            // 刻意不还原 HasAbility：还原后再次开启无法只靠写标志位重建组件，
            // 会变成"关了再开就失效"。副作用是关闭后仍保留普通二段跳。
        }

        // 由游戏主线程每个 FixedUpdate 调用
        private static void OnGameFixedUpdate()
        {
            // 游戏回调里抛出的异常会顺着 GameController.FixedUpdate 冒到 Unity，
            // 后果不可预期，必须自己兜住
            try
            {
                SeinCharacter sein = Game.Characters.Sein;
                if (sein == null) return;

                // ① 能力开关
                PlayerAbilities playerAbilities = sein.PlayerAbilities;
                if (playerAbilities == null) return;

                if (playerAbilities.DoubleJump != null)
                    playerAbilities.DoubleJump.HasAbility = true;

                // ② 实例化二段跳组件（本功能唯一需要主线程的一步）
                SeinPrefabFactory prefabs = sein.Prefabs;
                prefabs?.EnsureRightPrefabsAreThereForAbilities();

                // ③④ 跳跃次数与锁定时间（组件刚由 ② 实例化，此处已可读到）
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
