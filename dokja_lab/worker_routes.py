from flask import Blueprint, jsonify, render_template, request
from scheduler import (
    worker_status,
    get_scheduler,
    worker_configs,
)
import threading

workers_bp = Blueprint("workers", __name__)


@workers_bp.route("/health")
def health():
    """Retorna status em JSON para APIs"""
    all_statuses = [info["status"] for info in worker_status.values()]
    overall_status = "ok" if all(s == "success" for s in all_statuses) else "degraded"
    return jsonify({"status": overall_status, "workers": worker_status})


@workers_bp.route("/workers")
def index():
    """Página web mostrando status de todos os workers"""
    return render_template("workers.html", workers=worker_status)


@workers_bp.route("/worker/<worker_id>/pause", methods=["POST"])
def pause_worker(worker_id):
    scheduler = get_scheduler()
    try:
        scheduler.pause_job(worker_id)
        return jsonify({"success": True, "message": f"Worker {worker_id} paused."})
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/worker/<worker_id>/resume", methods=["POST"])
def resume_worker(worker_id):
    scheduler = get_scheduler()
    try:
        scheduler.resume_job(worker_id)
        return jsonify({"success": True, "message": f"Worker {worker_id} resumed."})
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/worker/<worker_id>/restart", methods=["POST"])
def restart_worker(worker_id):
    scheduler = get_scheduler()
    job = scheduler.get_job(worker_id)
    if not job:
        return jsonify({"success": False, "message": "Worker not found."}), 404
    try:
        # Remove and re-add the job with the same parameters
        job_args = job.args
        job_kwargs = job.kwargs
        job_func = job.func
        job_trigger = job.trigger
        job_id = job.id
        job_name = job.name
        job_trigger_args = (
            job.trigger.__getstate__() if hasattr(job.trigger, "__getstate__") else {}
        )
        scheduler.remove_job(worker_id)
        scheduler.add_job(
            job_func,
            trigger=job_trigger,
            args=job_args,
            kwargs=job_kwargs,
            id=job_id,
            name=job_name,
            **job_trigger_args,
        )
        return jsonify({"success": True, "message": f"Worker {worker_id} restarted."})
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400


@workers_bp.route("/worker/<worker_id>/execute_now", methods=["POST"])
def execute_now_worker(worker_id):
    config = worker_configs.get(worker_id)
    if not config:
        return (
            jsonify(
                {
                    "success": False,
                    "message": f"No config found for worker {worker_id}.",
                }
            ),
            404,
        )
    try:
        worker_class = config["worker_class"]
        init_args = config.get("init_args", [])
        init_kwargs = config.get("init_kwargs", {})
        worker_instance = worker_class(*init_args, **init_kwargs)

        def run_in_background():
            try:
                worker_instance.run()
            except Exception as e:
                # Optionally log the error
                pass

        threading.Thread(target=run_in_background, daemon=True).start()
        return jsonify(
            {"success": True, "message": f"Worker {worker_id} executed now."}
        )
    except Exception as e:
        return jsonify({"success": False, "message": str(e)}), 400
