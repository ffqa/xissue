class Xissue < Formula
  desc "Personal AI Agent Issue & Multi-Repo Orchestrator"
  homepage "https://github.com/ffqa/xissue"
  version "0.2.40"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.40/xissue-v0.2.40-darwin-arm64.tar.gz"
      sha256 "94d42eb625d575c18aa8b8491abe235697865bb9d6e99fa85ca8df86202a8ec5"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.40/xissue-v0.2.40-darwin-amd64.tar.gz"
      sha256 "5a13c725372dbdc634a8fd24da07f36a05e44ddd29ca78b2f5baa1a2315cec69"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/ffqa/xissue/releases/download/v0.2.40/xissue-v0.2.40-linux-arm64.tar.gz"
      sha256 "e5a175e36dfdc0ced8762f55e0794c5638e6da6f3bbe137995871cf252a30618"
    else
      url "https://github.com/ffqa/xissue/releases/download/v0.2.40/xissue-v0.2.40-linux-amd64.tar.gz"
      sha256 "cf303e9a3a763b955268c1a79430814ec6289328730a11687cb3e6507995ff1f"
    end
  end

  def install
    bin.install "xissue"
    (pkgshare/"scripts").install Dir["scripts/*"] if Dir.exist?("scripts")
    (pkgshare/"deploy").install Dir["deploy/*"] if Dir.exist?("deploy")
  end

  service do
    run [opt_bin/"xissue"]
    keep_alive true
    working_dir Dir.home
    environment_variables XISSUE_SERVICE: "1"
    log_path var/"log/xissue.log"
    error_log_path var/"log/xissue.log"
  end

  def caveats
    <<~EOS
      启动后台常驻服务方式（二选一）：
      1. Homebrew 服务管理:
         brew services start ffqa/tap/xissue
      2. xIssue 自管服务 (推荐，开机自启):
         xissue service install
    EOS
  end

  test do
    assert_match "xissue", shell_output("#{bin}/xissue version")
  end
end
